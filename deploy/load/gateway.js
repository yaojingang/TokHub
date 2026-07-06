import { baseURL, runLoad } from "./_lib.js";

const key = process.env.GATEWAY_KEY;
if (!key) {
  console.error("GATEWAY_KEY is required");
  process.exit(2);
}

await runLoad(
  "gateway-chat",
  () =>
    fetch(baseURL() + "/gateway/v1/chat/completions", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${key}`
      },
      body: JSON.stringify({
        model: process.env.LOAD_MODEL || "gpt-4o-mini",
        messages: [{ role: "user", content: "TokHub load probe" }]
      })
    }),
  { qps: 100, concurrency: 25 }
);
