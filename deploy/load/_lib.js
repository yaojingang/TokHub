export function envNumber(name, fallback) {
  const value = Number.parseInt(process.env[name] ?? "", 10);
  return Number.isFinite(value) && value > 0 ? value : fallback;
}

export function baseURL() {
  return process.env.TOKHUB_PUBLIC_URL || process.env.PLAYWRIGHT_BASE_URL || "http://localhost:8080";
}

export async function runLoad(name, requestFactory, options = {}) {
  const durationMs = envNumber("LOAD_DURATION_SECONDS", options.durationSeconds ?? 20) * 1000;
  const concurrency = envNumber("LOAD_CONCURRENCY", options.concurrency ?? 10);
  const targetQPS = envNumber("LOAD_QPS", options.qps ?? 100);
  const delayMs = Math.max(1, Math.floor((concurrency * 1000) / targetQPS));
  const endAt = Date.now() + durationMs;
  const stats = { ok: 0, failed: 0, latencies: [] };

  async function worker(id) {
    while (Date.now() < endAt) {
      const started = performance.now();
      try {
        const response = await requestFactory(id);
        const latency = performance.now() - started;
        stats.latencies.push(latency);
        if (response.ok) stats.ok += 1;
        else stats.failed += 1;
      } catch (error) {
        stats.failed += 1;
      }
      await new Promise((resolve) => setTimeout(resolve, delayMs));
    }
  }

  await Promise.all(Array.from({ length: concurrency }, (_, index) => worker(index)));
  stats.latencies.sort((a, b) => a - b);
  const p95 = stats.latencies[Math.floor(stats.latencies.length * 0.95)] ?? 0;
  const total = stats.ok + stats.failed;
  const errorRate = total ? stats.failed / total : 0;
  const result = { name, total, ok: stats.ok, failed: stats.failed, errorRate, p95Ms: Math.round(p95) };
  console.log(JSON.stringify(result, null, 2));
  if (errorRate > (Number.parseFloat(process.env.LOAD_MAX_ERROR_RATE ?? "0.05"))) {
    process.exitCode = 1;
  }
}
