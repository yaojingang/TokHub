import { expect, Page, test } from "@playwright/test";
import { execFile } from "node:child_process";
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { promisify } from "node:util";
import { adminLogin } from "./helpers";

test.describe.configure({ mode: "serial" });

const execFileAsync = promisify(execFile);

async function writeJSON(page: Page, path: string, method: string, body?: unknown) {
  const token = await page.evaluate(async () => {
    const response = await fetch("/api/auth/csrf", { credentials: "include" });
    return ((await response.json()) as { csrfToken: string }).csrfToken;
  });
  return page.evaluate(
    async ({ path: targetPath, method: targetMethod, body: payload, token: csrfToken }) => {
      const response = await fetch(targetPath, {
        method: targetMethod,
        credentials: "include",
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": csrfToken
        },
        body: payload === undefined ? undefined : JSON.stringify(payload)
      });
      return { ok: response.ok, status: response.status, payload: await response.json() };
    },
    { path, method, body, token }
  );
}

async function readJSON(page: Page, path: string) {
  return page.evaluate(async (targetPath) => {
    const response = await fetch(targetPath, { credentials: "include", cache: "no-store" });
    return { ok: response.ok, status: response.status, payload: await response.json() };
  }, path);
}

async function createAdminUser(page: Page, email: string, name: string, emailVerified = true) {
  const created = await writeJSON(page, "/api/admin/users", "POST", {
    email,
    password: "admin@tokhub.local",
    name,
    role: "user",
    status: "active",
    emailVerified,
    dataOrigin: "runtime"
  });
  expect(created.status).toBe(201);
  return created.payload.user as { id: string };
}

async function loginAs(page: Page, email: string) {
  const loggedIn = await writeJSON(page, "/api/auth/login", "POST", {
    email,
    password: "admin@tokhub.local"
  });
  expect(loggedIn.ok).toBeTruthy();
}

function readRequestBody(req: IncomingMessage) {
  return new Promise<string>((resolve) => {
    const chunks: Buffer[] = [];
    req.on("data", (chunk) => chunks.push(Buffer.from(chunk)));
    req.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
  });
}

function localPlaywrightTarget() {
  const baseURL = process.env.PLAYWRIGHT_BASE_URL ?? "http://localhost:8080";
  return /^https?:\/\/(localhost|127\.0\.0\.1)(?::|\/|$)/.test(baseURL);
}

async function purgePhase115Fixture(suffix: number) {
  if (!localPlaywrightTarget()) return;
  const fixtureSuffix = String(suffix);
  if (!/^\d+$/.test(fixtureSuffix)) throw new Error(`unsafe fixture suffix: ${fixtureSuffix}`);
  const sql = `
begin;
create temp table fixture_users as
  select id from users
  where email in (
    'phase115-owner-${fixtureSuffix}@example.com',
    'phase115-member-${fixtureSuffix}@example.com',
    'phase115-unverified-${fixtureSuffix}@example.com'
  );
create temp table fixture_orgs as
  select id from orgs
  where id in (select 'org_' || id from fixture_users)
     or name='Phase115 Workspace ${fixtureSuffix}';
create temp table fixture_channels as
  select id from channels
  where name='Phase115 Private ${fixtureSuffix}' or owner_id in (select id from fixture_users);
create temp table fixture_gateways as
  select id from gateways
  where name='Phase115 Gateway ${fixtureSuffix}' or created_by in (select id from fixture_users);
create temp table fixture_keys as
  select id from gateway_keys
  where gateway_id in (select id from fixture_gateways) or created_by in (select id from fixture_users);

delete from audit_events
where actor_id in (select id from fixture_users)
   or object_id in (
     select id from fixture_users
     union select id from fixture_orgs
     union select id from fixture_channels
     union select id from fixture_gateways
     union select id from fixture_keys
   )
   or metadata->>'gateway_id' in (select id from fixture_gateways)
   or metadata->>'channel_id' in (select id from fixture_channels);
delete from gateway_request_events
where gateway_id in (select id from fixture_gateways)
   or upstream_channel_id in (select id from fixture_channels)
   or gateway_key_id in (select id from fixture_keys);
delete from usage_daily_rollups
where gateway_id in (select id from fixture_gateways)
   or channel_id in (select id from fixture_channels)
   or member_user_id in (select id from fixture_users);
delete from gateway_upstreams
where gateway_id in (select id from fixture_gateways)
   or channel_id in (select id from fixture_channels);
delete from gateway_keys where id in (select id from fixture_keys);
delete from gateways where id in (select id from fixture_gateways);
delete from probe_results where channel_id in (select id from fixture_channels);
delete from probe_runs where channel_id in (select id from fixture_channels);
delete from channel_status_snapshots where channel_id in (select id from fixture_channels);
delete from favorites
where user_id in (select id from fixture_users)
   or channel_id in (select id from fixture_channels);
delete from channel_credentials
where channel_id in (select id from fixture_channels)
   or owner_id in (select id from fixture_users);
delete from channels where id in (select id from fixture_channels);
delete from org_members
where org_id in (select id from fixture_orgs)
   or user_id in (select id from fixture_users);
delete from auth_sessions where user_id in (select id from fixture_users);
delete from email_tokens where user_id in (select id from fixture_users);
delete from orgs where id in (select id from fixture_orgs);
delete from users where id in (select id from fixture_users);
commit;
`;
  await execFileAsync("docker", ["compose", "exec", "-T", "db", "psql", "-U", "tokhub", "-d", "tokhub", "-v", "ON_ERROR_STOP=1", "-c", sql], {
    cwd: process.cwd()
  });
}

test("phase 11.5 user workspace settings, members, private validation and gateway debug", async ({ page }) => {
  const suffix = Date.now();
  const ownerEmail = `phase115-owner-${suffix}@example.com`;
  const memberEmail = `phase115-member-${suffix}@example.com`;
  const unverifiedEmail = `phase115-unverified-${suffix}@example.com`;
  const cleanupUserIDs: string[] = [];
  let privateID: string | null = null;
  let gatewayID: string | null = null;
  let upstream: ReturnType<typeof createServer> | null = null;
  let upstreamStarted = false;

  try {
    await adminLogin(page, "/admin");
    const ownerUser = await createAdminUser(page, ownerEmail, "Phase115 Owner");
    cleanupUserIDs.push(ownerUser.id);
    const memberUser = await createAdminUser(page, memberEmail, "Phase115 Member");
    cleanupUserIDs.push(memberUser.id);
    const unverifiedUser = await createAdminUser(page, unverifiedEmail, "Phase115 Unverified", false);
    cleanupUserIDs.push(unverifiedUser.id);
    await loginAs(page, ownerEmail);

    const settings = await readJSON(page, "/api/console/settings");
    expect(settings.ok).toBeTruthy();
    const ownerMembers = await readJSON(page, "/api/console/members");
    expect(ownerMembers.ok).toBeTruthy();
    const ownerID = (ownerMembers.payload.members as Array<{ userId: string; email: string }>).find((member) => member.email === ownerEmail)?.userId;
    expect(ownerID).toBeTruthy();

    const selfInvite = await writeJSON(page, "/api/console/members", "POST", {
      email: ownerEmail,
      role: "viewer",
      groupName: "错误路径"
    });
    expect(selfInvite.ok).toBeFalsy();
    expect(selfInvite.status).toBe(400);

    const selfRoleChange = await writeJSON(page, `/api/console/members/${ownerID}`, "PATCH", {
      role: "viewer",
      groupName: "错误路径"
    });
    expect(selfRoleChange.ok).toBeFalsy();
    expect(selfRoleChange.status).toBe(400);

    const unverifiedInvite = await writeJSON(page, "/api/console/members", "POST", {
      email: unverifiedEmail,
      role: "viewer",
      groupName: "错误路径"
    });
    expect(unverifiedInvite.ok).toBeFalsy();
    expect(unverifiedInvite.status).toBe(403);
    expect(unverifiedInvite.payload.error.message).toBe("该邮箱需要完成邮箱验证后才能加入工作区");

    const missingMemberInvite = await writeJSON(page, "/api/console/members", "POST", {
      email: `phase115-missing-${suffix}@example.com`,
      role: "viewer",
      groupName: "错误路径"
    });
    expect(missingMemberInvite.ok).toBeFalsy();
    expect(missingMemberInvite.status).toBe(404);
    expect(missingMemberInvite.payload.error.message).toBe("该邮箱需要先注册并完成邮箱验证");

    const workspaceName = `Phase115 Workspace ${suffix}`;
    const updatedSettings = await writeJSON(page, "/api/console/settings", "PATCH", {
      name: workspaceName,
      timezone: "Asia/Shanghai",
      defaultGatewayPolicy: "success",
      defaultNotificationChannelId: ""
    });
    expect(updatedSettings.ok).toBeTruthy();
    expect(updatedSettings.payload.workspace.name).toBe(workspaceName);
    expect(updatedSettings.payload.workspace.defaultGatewayPolicy).toBe("success");

    const invited = await writeJSON(page, "/api/console/members", "POST", {
      email: memberEmail,
      role: "owner",
      groupName: "研发"
    });
    expect(invited.ok).toBeTruthy();
    const memberID = invited.payload.member.userId as string;
    expect(invited.payload.member.role).toBe("viewer");

    const roleUpdated = await writeJSON(page, `/api/console/members/${memberID}`, "PATCH", {
      role: "owner",
      groupName: "研发"
    });
    expect(roleUpdated.ok).toBeTruthy();
    expect(roleUpdated.payload.member.role).toBe("viewer");

    const operatorUpdated = await writeJSON(page, `/api/console/members/${memberID}`, "PATCH", {
      role: "operator",
      groupName: "研发"
    });
    expect(operatorUpdated.ok).toBeTruthy();
    expect(operatorUpdated.payload.member.role).toBe("operator");

    const calls: string[] = [];
    const upstreamSecret = `sk-phase115-${suffix}`;
    upstream = createServer((req: IncomingMessage, res: ServerResponse) => {
      void (async () => {
        const body = await readRequestBody(req);
        calls.push(`${req.method ?? ""} ${req.url ?? ""} ${body}`);
        if (req.headers.authorization !== `Bearer ${upstreamSecret}`) {
          res.writeHead(401, { "Content-Type": "application/json" });
          res.end(JSON.stringify({ error: "bad auth" }));
          return;
        }
        if (req.url === "/v1/models") {
          res.writeHead(200, { "Content-Type": "application/json" });
          res.end(JSON.stringify({ object: "list", data: [{ id: "gpt-phase115", object: "model", owned_by: "phase115" }] }));
          return;
        }
        if (req.url === "/v1/chat/completions") {
          if (body.includes("Reply exactly: K")) {
            res.writeHead(200, { "Content-Type": "application/json" });
            res.end(JSON.stringify({
              id: "chatcmpl-phase115-probe",
              object: "chat.completion",
              model: "gpt-phase115",
              choices: [{ index: 0, message: { role: "assistant", content: "K" }, finish_reason: "stop" }],
              usage: { prompt_tokens: 4, completion_tokens: 1, total_tokens: 5 }
            }));
            return;
          }
          res.writeHead(200, { "Content-Type": "application/json" });
          res.end(JSON.stringify({
            id: "chatcmpl-phase115",
            object: "chat.completion",
            model: "gpt-phase115",
            choices: [{ index: 0, message: { role: "assistant", content: "OK" }, finish_reason: "stop" }],
            usage: { prompt_tokens: 9, completion_tokens: 2, total_tokens: 11 }
          }));
          return;
        }
        res.writeHead(404, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ error: "not found" }));
      })();
    });
    await new Promise<void>((resolve) => upstream!.listen(0, "0.0.0.0", resolve));
    upstreamStarted = true;
    const address = upstream.address();
    if (typeof address !== "object" || address === null) throw new Error("upstream failed to bind");
    const upstreamHost = process.env.TOKHUB_TEST_UPSTREAM_HOST || "host.docker.internal";
    const endpointRoot = `http://${upstreamHost}:${address.port}`;

    const draftValidation = await writeJSON(page, "/api/me/private-channels/validate", "POST", {
      provider: "OpenAI",
      type: "openai-compatible",
      model: "gpt-phase115",
      endpoint: endpointRoot,
      apiKey: upstreamSecret
    });
    expect(draftValidation.ok).toBeTruthy();
    expect(draftValidation.payload.result.ok).toBeTruthy();
    expect(calls.some((call) => call.includes("GET /v1/models"))).toBeTruthy();
    expect(calls.some((call) => call.includes("POST /v1/chat/completions"))).toBeTruthy();

    const createdPrivate = await writeJSON(page, "/api/me/private-channels", "POST", {
      name: `Phase115 Private ${suffix}`,
      provider: "OpenAI",
      type: "openai-compatible",
      model: "gpt-phase115",
      endpoint: endpointRoot,
      apiKey: upstreamSecret,
      probeDaily: 10
    });
    expect(createdPrivate.ok).toBeTruthy();
    expect(JSON.stringify(createdPrivate.payload)).not.toContain(upstreamSecret);
    privateID = createdPrivate.payload.channel.id as string;

    const savedValidation = await writeJSON(page, `/api/me/private-channels/${privateID}/validate`, "POST", {});
    expect(savedValidation.ok).toBeTruthy();
    expect(savedValidation.payload.result.ok).toBeTruthy();
    const privateProbe = await writeJSON(page, `/api/me/private-channels/${privateID}/probe-now`, "POST", {});
    expect(privateProbe.ok).toBeTruthy();

    const gateway = await writeJSON(page, "/api/console/gateways", "POST", {
      name: `Phase115 Gateway ${suffix}`,
      policy: "success",
      upstreamIds: [privateID],
      qpsLimit: 20,
      quotaMonth: 1000
    });
    expect(gateway.ok).toBeTruthy();
    gatewayID = gateway.payload.gateway.id as string;

    const debug = await writeJSON(page, `/api/console/gateways/${gatewayID}/debug`, "POST", {
      model: "gpt-phase115",
      prompt: "Reply exactly: OK"
    });
    expect(debug.ok).toBeTruthy();
    expect(debug.payload.result.ok).toBeTruthy();
    expect(debug.payload.result.preview).toContain("OK");

    const usage = await readJSON(page, "/api/console/usage");
    expect(usage.ok).toBeTruthy();
    expect(usage.payload.recent.some((event: { gateway: string; channel: string; tokens: number; statusCode: number }) =>
      event.gateway === `Phase115 Gateway ${suffix}` &&
      event.channel === `Phase115 Private ${suffix}` &&
      event.tokens === 11 &&
      event.statusCode === 200
    )).toBeTruthy();

    const removed = await writeJSON(page, `/api/console/members/${memberID}`, "DELETE", {});
    expect(removed.ok).toBeTruthy();
    const members = await readJSON(page, "/api/console/members");
    expect(JSON.stringify(members.payload.members)).not.toContain(memberEmail);
  } finally {
    if (gatewayID) {
      await writeJSON(page, `/api/console/gateways/${gatewayID}`, "DELETE", {}).catch(() => undefined);
    }
    if (privateID) {
      await writeJSON(page, `/api/me/private-channels/${privateID}`, "DELETE", {}).catch(() => undefined);
    }
    if (cleanupUserIDs.length) {
      await adminLogin(page, "/admin/users").catch(() => undefined);
      for (const userID of cleanupUserIDs) {
        await writeJSON(page, `/api/admin/orgs/org_${userID}`, "DELETE", {}).catch(() => undefined);
        await writeJSON(page, `/api/admin/users/${userID}`, "DELETE", {}).catch(() => undefined);
      }
    }
    if (upstream && upstreamStarted) {
      await new Promise<void>((resolve) => upstream!.close(() => resolve()));
    }
    await purgePhase115Fixture(suffix);
  }
});
