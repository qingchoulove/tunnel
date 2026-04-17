/**
 * Tunnel Signal Server — Cloudflare Worker
 *
 * API:
 *   PUT  /signal/:room/:role   Upload NATDetail for this role ("server" or "client")
 *   GET  /signal/:room/:role   Long-poll for the peer's NATDetail (opposite role)
 *
 * KV binding: SIGNAL_KV
 * Key format: <room>:<role>   Value: NATDetail JSON   TTL: 120s
 *
 * Deploy:
 *   1. Create a KV namespace named SIGNAL_KV in Cloudflare dashboard
 *   2. wrangler kv:namespace create SIGNAL_KV
 *   3. Add binding to wrangler.toml:
 *        [[kv_namespaces]]
 *        binding = "SIGNAL_KV"
 *        id = "<your-kv-id>"
 *   4. wrangler deploy
 */

const TTL = 120;          // seconds a signal entry lives in KV
const POLL_INTERVAL = 500; // ms between KV reads during long-poll
const POLL_TIMEOUT = 60;  // seconds before long-poll gives up

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    const parts = url.pathname.split("/").filter(Boolean);

    // Expected: /signal/:room/:role
    if (parts.length !== 3 || parts[0] !== "signal") {
      return new Response("Not Found", { status: 404 });
    }

    const [, room, role] = parts;
    if (role !== "server" && role !== "client") {
      return new Response("role must be 'server' or 'client'", { status: 400 });
    }

    if (request.method === "PUT") {
      return handlePut(request, env, room, role);
    }
    if (request.method === "GET") {
      return handleGet(request, env, room, role);
    }
    return new Response("Method Not Allowed", { status: 405 });
  },
};

async function handlePut(request, env, room, role) {
  const body = await request.text();
  try {
    JSON.parse(body); // validate
  } catch {
    return new Response("invalid JSON", { status: 400 });
  }
  await env.SIGNAL_KV.put(`${room}:${role}`, body, { expirationTtl: TTL });
  return new Response("ok");
}

// Long-poll: keep reading KV until the peer's entry appears or timeout.
async function handleGet(request, env, room, role) {
  const peerRole = role === "server" ? "client" : "server";
  const key = `${room}:${peerRole}`;
  const deadline = Date.now() + POLL_TIMEOUT * 1000;

  while (Date.now() < deadline) {
    const value = await env.SIGNAL_KV.get(key);
    if (value !== null) {
      return new Response(value, {
        headers: { "Content-Type": "application/json" },
      });
    }
    await sleep(POLL_INTERVAL);
  }
  return new Response("timeout waiting for peer", { status: 408 });
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
