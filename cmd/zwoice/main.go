package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/mail"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/iamrobertduda/Zwoice/internal/accounts"
	"github.com/iamrobertduda/Zwoice/internal/broker"
	"github.com/iamrobertduda/Zwoice/internal/desktop"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	mode := "desktop"
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		mode = args[0]
		args = args[1:]
	}
	if mode == "version" {
		fmt.Println("Zwoice", version)
		return nil
	}
	flags := flag.NewFlagSet(mode, flag.ContinueOnError)
	port := flags.Int("port", 8787, "local UI port")
	open := flags.Bool("open", true, "open the interface in the default browser")
	if err := flags.Parse(args); err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var handler http.Handler
	var address string
	switch mode {
	case "server":
		dataDir := os.Getenv("ZWOICE_DATA_DIR")
		if dataDir == "" {
			return fmt.Errorf("ZWOICE_DATA_DIR must point to persistent account storage")
		}
		if os.Getenv("RAILWAY_PROJECT_ID") != "" && os.Getenv("RAILWAY_VOLUME_MOUNT_PATH") != dataDir {
			return fmt.Errorf("ZWOICE_DATA_DIR must match the Railway volume mount path")
		}
		acfg := accounts.Config{Path: filepath.Join(dataDir, "accounts.db"), AdminEmail: os.Getenv("ZWOICE_ADMIN_EMAIL")}
		if limit := os.Getenv("ZWOICE_EMAIL_DAILY_LIMIT"); limit != "" {
			var err error
			acfg.DailyEmailLimit, err = strconv.Atoi(limit)
			if err != nil || acfg.DailyEmailLimit < 1 {
				return fmt.Errorf("invalid ZWOICE_EMAIL_DAILY_LIMIT")
			}
		}
		apiKey, from := os.Getenv("ZWOICE_RESEND_API_KEY"), os.Getenv("ZWOICE_MAIL_FROM")
		if apiKey != "" && from != "" {
			if _, err := mail.ParseAddress(from); err != nil {
				return fmt.Errorf("invalid ZWOICE_MAIL_FROM")
			}
			acfg.Mailer = &accounts.Resend{APIKey: apiKey, From: from}
		} else {
			log.Print("Login email delivery is not configured; authentication remains closed")
		}
		store, err := accounts.Open(acfg)
		if err != nil {
			return fmt.Errorf("account storage: %w", err)
		}
		defer store.Close()
		cfg := broker.Config{Accounts: store, STUNURLs: splitEnv("ZWOICE_STUN_URLS"), TURNURLs: splitEnv("ZWOICE_TURN_URLS"), TURNSecret: os.Getenv("ZWOICE_TURN_SECRET"), CloudflareKeyID: os.Getenv("ZWOICE_CF_TURN_KEY_ID"), CloudflareAPIToken: os.Getenv("ZWOICE_CF_TURN_API_TOKEN")}
		cfg.TrustRailwayProxy = os.Getenv("RAILWAY_PROJECT_ID") != ""
		if len(cfg.STUNURLs) == 0 {
			cfg.STUNURLs = []string{"stun:stun.cloudflare.com:3478"}
		}
		if (cfg.CloudflareKeyID == "") != (cfg.CloudflareAPIToken == "") {
			return fmt.Errorf("both Cloudflare TURN variables are required")
		}
		if len(cfg.TURNURLs) == 0 && cfg.CloudflareKeyID == "" {
			log.Print("TURN not configured: restrictive networks may fail to connect")
		}
		hub := broker.New(cfg)
		handler = hub.Handler()
		go hub.Run(ctx)
		p := os.Getenv("PORT")
		if p == "" {
			p = "8080"
		}
		address = net.JoinHostPort("0.0.0.0", p)
	case "desktop":
		if *port < 1024 || *port > 65535 {
			return fmt.Errorf("port must be between 1024 and 65535")
		}
		address = fmt.Sprintf("127.0.0.1:%d", *port)
		handler = desktop.Handler(address, cancel)
	default:
		return fmt.Errorf("usage: zwoice [desktop|server|version] [flags]")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		if mode == "desktop" {
			client := http.Client{Timeout: time.Second}
			if res, e := client.Get("http://" + address + "/healthz"); e == nil {
				var health struct {
					Service string `json:"service"`
				}
				json.NewDecoder(res.Body).Decode(&health)
				res.Body.Close()
				if health.Service == "zwoice-desktop" {
					if *open {
						openBrowser("http://" + address)
					}
					return nil
				}
			}
		}
		return fmt.Errorf("cannot listen on %s: %w", address, err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 8 * 1024}
	go func() {
		<-ctx.Done()
		shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		server.Shutdown(shutdown)
	}()
	if mode == "desktop" {
		url := "http://" + address
		log.Printf("Zwoice %s · %s · Ctrl+C to quit", version, url)
		if *open {
			go openBrowser(url)
		}
	} else {
		log.Printf("Zwoice signaling %s listening on %s", version, address)
	}
	err = server.Serve(listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func splitEnv(key string) []string {
	var values []string
	for _, v := range strings.Split(os.Getenv(key), ",") {
		if v = strings.TrimSpace(v); v != "" {
			values = append(values, v)
		}
	}
	return values
}

func openBrowser(url string) {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", url)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		command = exec.Command("xdg-open", url)
	}
	if err := command.Run(); err != nil {
		log.Print("Open ", url, " in your browser")
	}
}
