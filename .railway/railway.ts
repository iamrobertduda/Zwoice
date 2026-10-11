import { defineRailway, preserve, project, service } from "railway/iac";

// This repository manages only its own resources in the environment. Other
// repositories export their own partial name.
// See https://docs.railway.com/infrastructure-as-code#multi-repo-projects
export const partial = "zwoice";

export default defineRailway(() => {
  const zwoice = service("zwoice", {
    healthcheck: "/healthz",
    healthcheckTimeout: 30,
    replicas: 1,
    env: {
      ZWOICE_SIGNING_KEY: preserve(),
      ZWOICE_CF_TURN_KEY_ID: preserve(),
      ZWOICE_CF_TURN_API_TOKEN: preserve(),
    },
    build: { builder: "DOCKERFILE", dockerfilePath: "Dockerfile" },
    // Railway defaults to On Failure, with ten restart attempts.
    deploy: { drainingSeconds: 10 },
  });
  return project("Zwoice", {
    resources: [zwoice],
  });
});
