export interface Env {
  MESSAGES: KVNamespace;
  SESSIONS: DurableObjectNamespace;
  RATE_LIMITER: DurableObjectNamespace;
  WEBHOOK_SECRET: string;
  DAILY_LIMIT?: string;
  MONTHLY_LIMIT?: string;
  CHANNEL_URL?: string;
}

interface KakaoPayload {
  action: { id: string };
  userRequest: {
    utterance: string;
    user: { id: string };
    callbackUrl?: string;
  };
}

// ── RateLimiter Durable Object ───────────────────────────────

/**
 * Global singleton DO for daily/monthly hard cap counting.
 * Uses idFromName("global") — one instance for the entire relay.
 *
 * Routes:
 *   POST /check  { dailyLimit, monthlyLimit } → { ok, daily, monthly }
 *   GET  /stats                               → { daily, monthly }
 *   POST /reset                               → {} (clears all counters; for testing)
 */
export class RateLimiter implements DurableObject {
  private state: DurableObjectState;

  constructor(state: DurableObjectState) {
    this.state = state;
  }

  async fetch(request: Request): Promise<Response> {
    const url = new URL(request.url);

    if (url.pathname === "/check" && request.method === "POST") {
      return this.handleCheck(request);
    }
    if (url.pathname === "/stats" && request.method === "GET") {
      return this.handleStats();
    }
    if (url.pathname === "/reset" && request.method === "POST") {
      await this.state.storage.deleteAll();
      return Response.json({ ok: true });
    }

    return new Response("Not Found", { status: 404 });
  }

  private async handleCheck(request: Request): Promise<Response> {
    const { dailyLimit, monthlyLimit } = await request.json<{
      dailyLimit: number;
      monthlyLimit: number;
    }>();

    const now = new Date();
    const dayKey = `d:${now.toISOString().slice(0, 10)}`;
    const monthKey = `m:${now.toISOString().slice(0, 7)}`;

    const [d, m] = await Promise.all([
      this.state.storage.get<number>(dayKey),
      this.state.storage.get<number>(monthKey),
    ]);

    const daily = d ?? 0;
    const monthly = m ?? 0;

    if (daily >= dailyLimit || monthly >= monthlyLimit) {
      return Response.json({ ok: false, daily, monthly });
    }

    await Promise.all([
      this.state.storage.put(dayKey, daily + 1),
      this.state.storage.put(monthKey, monthly + 1),
    ]);

    return Response.json({ ok: true, daily: daily + 1, monthly: monthly + 1 });
  }

  private async handleStats(): Promise<Response> {
    const now = new Date();
    const dayKey = `d:${now.toISOString().slice(0, 10)}`;
    const monthKey = `m:${now.toISOString().slice(0, 7)}`;

    const [d, m] = await Promise.all([
      this.state.storage.get<number>(dayKey),
      this.state.storage.get<number>(monthKey),
    ]);

    return Response.json({ daily: d ?? 0, monthly: m ?? 0 });
  }
}

// ── KittyPawSession Durable Object ──────────────────────────

/**
 * One DO instance per user token. Maintains the WebSocket connection with
 * the KittyPaw desktop app and dispatches Kakao callbacks.
 *
 * Message flow:
 *   Kakao → Worker → DO.fetch(POST /message) → ws.send(frame)
 *   KittyPaw → DO.webSocketMessage({id, text}) → fetch(callbackUrl)
 *
 * Uses the WebSocket Hibernation API so the DO sleeps when idle.
 */
export class KittyPawSession implements DurableObject {
  private state: DurableObjectState;

  constructor(state: DurableObjectState) {
    this.state = state;
  }

  async fetch(request: Request): Promise<Response> {
    if (request.headers.get("Upgrade")?.toLowerCase() === "websocket") {
      return this.handleWsUpgrade();
    }

    const url = new URL(request.url);
    if (url.pathname === "/message" && request.method === "POST") {
      return this.handleMessage(request);
    }

    return new Response("Not Found", { status: 404 });
  }

  private handleWsUpgrade(): Response {
    const pair = new WebSocketPair();
    const [client, server] = Object.values(pair) as [WebSocket, WebSocket];
    this.state.acceptWebSocket(server);
    return new Response(null, { status: 101, webSocket: client });
  }

  private async handleMessage(request: Request): Promise<Response> {
    const body = await request.json<{
      actionId: string;
      utterance: string;
      userId: string;
      callbackUrl: string;
    }>();

    const sockets = this.state.getWebSockets();
    if (sockets.length === 0) {
      return Response.json({ offline: true });
    }

    // Store callback context before pushing to app (survives hibernation)
    await this.state.storage.put(
      `pending:${body.actionId}`,
      JSON.stringify({ callbackUrl: body.callbackUrl, userId: body.userId })
    );

    sockets[0].send(
      JSON.stringify({
        id: body.actionId,
        text: body.utterance,
        user_id: body.userId,
      })
    );

    return Response.json({ ok: true });
  }

  /** Called by the hibernation runtime when the app sends a response frame. */
  async webSocketMessage(
    _ws: WebSocket,
    message: string | ArrayBuffer
  ): Promise<void> {
    let data: { id?: string; text?: string };
    try {
      const raw =
        typeof message === "string"
          ? message
          : new TextDecoder().decode(message);
      data = JSON.parse(raw) as { id?: string; text?: string };
    } catch {
      console.warn("[DO] malformed WS frame, ignoring");
      return;
    }

    if (!data.id || !data.text) {
      console.warn("[DO] WS frame missing id/text, ignoring");
      return;
    }

    // delete-before-dispatch: atomically claim the pending entry so we never
    // fire the Kakao callback twice (e.g. on hibernation resume with retry).
    const rawPending = await this.state.storage.get<string>(
      `pending:${data.id}`
    );
    await this.state.storage.delete(`pending:${data.id}`);

    if (!rawPending) {
      console.warn(`[DO] no pending entry for action ${data.id}, skipping`);
      return;
    }

    let pending: { callbackUrl: string; userId: string };
    try {
      pending = JSON.parse(rawPending) as {
        callbackUrl: string;
        userId: string;
      };
    } catch {
      console.warn("[DO] corrupt pending entry, skipping");
      return;
    }

    // SSRF guard: only dispatch to Kakao's callback domain
    let callbackHost: string;
    try {
      callbackHost = new URL(pending.callbackUrl).hostname;
    } catch {
      console.warn("[DO] invalid callbackUrl, skipping");
      return;
    }
    if (!callbackHost.endsWith("kakao.com") && !callbackHost.endsWith("kakaoenterprise.com")) {
      console.warn(`[DO] callbackUrl host ${callbackHost} not in allowlist, skipping`);
      return;
    }

    await fetch(pending.callbackUrl, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        version: "2.0",
        template: {
          outputs: [{ simpleText: { text: data.text } }],
        },
      }),
    });
  }

  async webSocketClose(_ws: WebSocket): Promise<void> {
    // Hibernation handles socket cleanup; no action needed.
  }

  async webSocketError(_ws: WebSocket, _error: unknown): Promise<void> {
    // Hibernation handles error recovery; no action needed.
  }
}

// ── Cost Protection ─────────────────────────────────────────

function getLimits(env: Env) {
  return {
    daily: Number(env.DAILY_LIMIT) || 10_000,
    monthly: Number(env.MONTHLY_LIMIT) || 100_000,
  };
}

/** Daily + monthly hard cap via RateLimiter DO (zero KV writes). */
async function checkHardCap(
  env: Env
): Promise<{ ok: boolean; daily: number; monthly: number }> {
  const limits = getLimits(env);
  const stub = env.RATE_LIMITER.get(env.RATE_LIMITER.idFromName("global"));

  const res = await stub.fetch(
    new Request("https://do-internal/check", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        dailyLimit: limits.daily,
        monthlyLimit: limits.monthly,
      }),
    })
  );

  return res.json<{ ok: boolean; daily: number; monthly: number }>();
}

// ── Main Router ─────────────────────────────────────────────

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);

    // Admin routes — always accessible (auth via secret)
    if (url.pathname.startsWith("/admin/")) {
      return handleAdmin(request, env, url);
    }

    // WebSocket upgrade — KittyPaw app connects here; bypass killswitch
    if (request.headers.get("Upgrade")?.toLowerCase() === "websocket") {
      const wsMatch = url.pathname.match(/^\/ws\/(.+)$/);
      if (wsMatch) {
        return handleWsConnect(request, env, wsMatch[1]);
      }
      return new Response("Not Found", { status: 404 });
    }

    // Layer 3: Killswitch — blocks all non-admin, non-WS routes
    if ((await env.MESSAGES.get("rl:killswitch")) === "1") {
      return new Response("Service temporarily suspended", { status: 503 });
    }

    // Pair-status polling — GUI checks whether pairing completed
    const pairStatusMatch = url.pathname.match(/^\/pair-status\/([a-f0-9]+)$/);
    if (pairStatusMatch && request.method === "GET") {
      return handlePairStatus(env, pairStatusMatch[1]);
    }

    if (request.method !== "POST") {
      return new Response("Not Found", { status: 404 });
    }

    if (url.pathname === "/register") {
      return handleRegister(env);
    }

    if (url.pathname === "/webhook") {
      // Daily/monthly hard cap
      const cap = await checkHardCap(env);
      if (!cap.ok) {
        return kakaoText("일일 사용 한도에 도달했습니다.");
      }

      return handleWebhook(request, env, url);
    }

    return new Response("Not Found", { status: 404 });
  },
};

// ── Admin ────────────────────────────────────────────────────

async function handleAdmin(
  request: Request,
  env: Env,
  url: URL
): Promise<Response> {
  const secret = url.searchParams.get("secret");
  if (!env.WEBHOOK_SECRET || !secret || secret !== env.WEBHOOK_SECRET) {
    return new Response("Unauthorized", { status: 401 });
  }

  if (url.pathname === "/admin/killswitch" && request.method === "POST") {
    const body = (await request.json()) as { enabled: boolean };
    if (body.enabled) {
      await env.MESSAGES.put("rl:killswitch", "1");
    } else {
      await env.MESSAGES.delete("rl:killswitch");
    }
    return Response.json({ killswitch: body.enabled });
  }

  if (url.pathname === "/admin/stats" && request.method === "GET") {
    const limits = getLimits(env);
    const stub = env.RATE_LIMITER.get(
      env.RATE_LIMITER.idFromName("global")
    );

    const [statsRes, killed] = await Promise.all([
      stub.fetch(new Request("https://do-internal/stats")),
      env.MESSAGES.get("rl:killswitch"),
    ]);

    const stats = await statsRes.json<{ daily: number; monthly: number }>();

    return Response.json({
      daily: { current: stats.daily, limit: limits.daily },
      monthly: { current: stats.monthly, limit: limits.monthly },
      killswitch: killed === "1",
    });
  }

  return new Response("Not Found", { status: 404 });
}

// ── Kakao Handlers ───────────────────────────────────────────

function kakaoText(text: string): Response {
  return Response.json({
    version: "2.0",
    template: { outputs: [{ simpleText: { text } }] },
  });
}

async function handleWsConnect(
  request: Request,
  env: Env,
  token: string
): Promise<Response> {
  const tokenExists = await env.MESSAGES.get(`tok:${token}`);
  if (!tokenExists) {
    return new Response("Unauthorized", { status: 401 });
  }

  const id = env.SESSIONS.idFromName(token);
  const stub = env.SESSIONS.get(id);
  return stub.fetch(request);
}

async function handleRegister(env: Env): Promise<Response> {
  const token = crypto.randomUUID().replace(/-/g, "");
  const buf = new Uint32Array(1);
  crypto.getRandomValues(buf);
  const pairCode = String(100000 + (buf[0] % 900000));

  await Promise.all([
    env.MESSAGES.put(`tok:${token}`, "1"),
    env.MESSAGES.put(`pair:${pairCode}`, token, { expirationTtl: 300 }),
  ]);

  return Response.json({
    token,
    pair_code: pairCode,
    channel_url: env.CHANNEL_URL || "",
  });
}

async function handleWebhook(
  request: Request,
  env: Env,
  url: URL
): Promise<Response> {
  const secret = url.searchParams.get("secret");
  if (!env.WEBHOOK_SECRET || !secret || secret !== env.WEBHOOK_SECRET) {
    return new Response("Unauthorized", { status: 401 });
  }

  let payload: KakaoPayload;
  try {
    payload = (await request.json()) as KakaoPayload;
  } catch {
    return new Response("Bad Request", { status: 400 });
  }

  const actionId = payload.action?.id;
  const utterance = payload.userRequest?.utterance;
  const userId = payload.userRequest?.user?.id;
  const callbackUrl = payload.userRequest?.callbackUrl;

  if (!actionId || !utterance || !userId) {
    return new Response("Bad Request: missing required fields", { status: 400 });
  }

  // Pairing: 6-digit utterance → attempt to match a pair code
  if (/^\d{6}$/.test(utterance)) {
    return handlePairing(env, utterance, userId);
  }

  // No callbackUrl = synchronous test mode (OpenBuilder test tool)
  if (!callbackUrl) {
    return kakaoText(
      "KittyPaw 스킬 서버가 정상 동작 중입니다. 오픈빌더에서 비동기 콜백을 활성화하면 AI 응답을 받을 수 있습니다."
    );
  }

  // Routing: look up user → relay token mapping
  const relayToken = await env.MESSAGES.get(`user:${userId}`);
  if (!relayToken) {
    return kakaoText(
      "KittyPaw와 연결이 필요합니다. KittyPaw 앱에서 연결 코드를 확인하세요."
    );
  }

  // Forward message to KittyPawSession DO
  const sessionId = env.SESSIONS.idFromName(relayToken);
  const stub = env.SESSIONS.get(sessionId);

  let doResult: { ok?: boolean; offline?: boolean };
  try {
    const doResp = await stub.fetch(
      new Request("https://do-internal/message", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ actionId, utterance, userId, callbackUrl }),
      })
    );
    doResult = (await doResp.json()) as { ok?: boolean; offline?: boolean };
  } catch (e) {
    console.error("[webhook] DO fetch error:", e);
    return kakaoText(
      "일시적인 오류가 발생했습니다. 잠시 후 다시 시도해주세요."
    );
  }

  if (doResult.offline) {
    return kakaoText(
      "KittyPaw가 현재 오프라인 상태입니다. 앱을 실행 후 다시 시도해 주세요."
    );
  }

  return Response.json({
    version: "2.0",
    useCallback: true,
    data: { text: "처리 중입니다..." },
  });
}

async function handlePairing(
  env: Env,
  pairCode: string,
  kakaoUserId: string
): Promise<Response> {
  const token = await env.MESSAGES.get(`pair:${pairCode}`);
  if (!token) {
    return kakaoText(
      "유효하지 않은 연결 코드입니다. KittyPaw 앱에서 새 코드를 확인하세요."
    );
  }

  await Promise.all([
    env.MESSAGES.put(`user:${kakaoUserId}`, token),
    env.MESSAGES.delete(`pair:${pairCode}`),
    env.MESSAGES.put(`paired:${token}`, "1", { expirationTtl: 600 }),
  ]);

  return kakaoText("연결 완료!");
}

async function handlePairStatus(env: Env, token: string): Promise<Response> {
  const marker = await env.MESSAGES.get(`paired:${token}`);
  return new Response(JSON.stringify({ paired: marker === "1" }), {
    headers: { "Content-Type": "application/json" },
  });
}
