import { baseURL, runLoad } from "./_lib.js";

const channelID = process.env.CHANNEL_ID || "ch_openai_gpt4o";
const csrf = process.env.CSRF_TOKEN || "";
const cookie = process.env.COOKIE || "";

await runLoad(
  "prober-write",
  () =>
    fetch(baseURL() + `/api/admin/channels/${channelID}/probe-now`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrf,
        Cookie: cookie
      },
      body: "{}"
    }),
  { qps: 20, concurrency: 5 }
);
