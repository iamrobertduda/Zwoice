package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/iamrobertduda/Zwoice/internal/auth"
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
	athlete := flags.Int64("athlete", 0, "Zwift athlete ID for an invitation")
	cohort := flags.String("group", "public", "invitation group; riders only hear others in the same group")
	duration := flags.Duration("valid", 30*24*time.Hour, "invitation validity")
	if err := flags.Parse(args); err != nil {
		return err
	}
	key := []byte(os.Getenv("ZWOICE_SIGNING_KEY"))
	if mode == "invite" {
		if *duration <= 0 || *duration > 365*24*time.Hour {
			return fmt.Errorf("invitation validity must be between 0 and 365 days")
		}
		token, err := auth.Sign(key, auth.Claims{AthleteID: *athlete, Cohort: *cohort, Expires: time.Now().Add(*duration).Unix()})
		if err != nil {
			return err
		}
		fmt.Println(token)
		return nil
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var handler http.Handler
	var address string
	switch mode {
	case "server":
		if len(key) != 0 && len(key) < 32 {
			return fmt.Errorf("optional ZWOICE_SIGNING_KEY must contain at least 32 bytes")
		}
		cfg := broker.Config{Key: key, STUNURLs: splitEnv("ZWOICE_STUN_URLS"), TURNURLs: splitEnv("ZWOICE_TURN_URLS"), TURNSecret: os.Getenv("ZWOICE_TURN_SECRET"), CloudflareKeyID: os.Getenv("ZWOICE_CF_TURN_KEY_ID"), CloudflareAPIToken: os.Getenv("ZWOICE_CF_TURN_API_TOKEN")}
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
		return fmt.Errorf("usage: zwoice [desktop|server|invite|version] [flags]")
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
