import { baseURL, runLoad } from "./_lib.js";

const base = baseURL();
const paths = [
  "/api/public/overview",
  "/api/public/channels?page=1&pageSize=20",
  "/api/public/providers/rank",
  "/api/public/errors/summary"
];

const openAPIKey = process.env.OPEN_API_SITE_KEY || "";
if (openAPIKey) {
  paths.push("/v1/status/overview");
}

await runLoad(
  "public-api",
  (id) => {
    const path = paths[id % paths.length];
    const headers = path.startsWith("/v1/status/") && openAPIKey ? { "X-Site-Key": openAPIKey } : undefined;
    return fetch(base + path, { headers });
  },
  { qps: 4, concurrency: 2 }
);
