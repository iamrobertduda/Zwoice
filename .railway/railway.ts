import {
  defineRailway,
  preserve,
  project,
  service,
  volume,
  github,
} from "railway/iac";

// This repository manages only its own resources in the environment. Other
// repositories export their own partial name.
// See https://docs.railway.com/infrastructure-as-code#multi-repo-projects
export const partial = "zwoice";

export default defineRailway(() => {
  const accounts = volume("zwoice-accounts", {
    sizeMB: 1024,
    region: "europe-west4-drams3a",
  });
  const zwoice = service("zwoice", {
    source: github("iamrobertduda/Zwoice", {
      branch: "main",
      checkSuites: false,
    }),
    healthcheck: "/healthz",
    healthcheckTimeout: 30,
    replicas: 1,
    env: {
      ZWOICE_SIGNING_KEY: preserve(), // Retained for migration; never accepted as login.
      ZWOICE_DATA_DIR: "/data",
      ZWOICE_RESEND_API_KEY: preserve(),
      ZWOICE_MAIL_FROM: preserve(),
      ZWOICE_ADMIN_EMAIL: preserve(),
      ZWOICE_EMAIL_DAILY_LIMIT: preserve(),
      ZWOICE_CF_TURN_KEY_ID: preserve(),
      ZWOICE_CF_TURN_API_TOKEN: preserve(),
    },
    volumeMounts: { "/data": accounts },
    build: { builder: "DOCKERFILE", dockerfilePath: "Dockerfile" },
    // Railway defaults to On Failure, with ten restart attempts.
    deploy: { drainingSeconds: 10 },
  });
  return project("Zwoice", {
    resources: [zwoice, accounts],
  });
});
