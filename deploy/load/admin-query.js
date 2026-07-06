import { baseURL, runLoad } from "./_lib.js";

const cookie = process.env.COOKIE || "";
const paths = [
  "/api/admin/channels",
  "/api/admin/usage",
  "/api/admin/alerts",
  "/api/admin/audit?limit=50",
  "/api/admin/governance/summary"
];

await runLoad(
  "admin-query",
  (id) =>
    fetch(baseURL() + paths[id % paths.length], {
      headers: { Cookie: cookie }
    }),
  { qps: 50, concurrency: 10 }
);
