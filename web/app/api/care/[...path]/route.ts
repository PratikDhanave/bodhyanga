import { type NextRequest } from "next/server";

// Same-origin proxy to the care API (cmd/care), which binds loopback and sends
// no CORS headers. The browser calls /api/care/* here; this forwards to the Go
// server server-side. Override the target with CARE_API_URL.
const CARE = process.env.CARE_API_URL ?? "http://localhost:8088";

async function handler(
  req: NextRequest,
  ctx: { params: Promise<{ path: string[] }> },
) {
  const { path } = await ctx.params;
  const url = `${CARE}/${path.join("/")}${req.nextUrl.search}`;

  const init: RequestInit = {
    method: req.method,
    cache: "no-store",
    headers: { "content-type": "application/json" },
  };
  if (req.method !== "GET" && req.method !== "HEAD") {
    init.body = await req.text();
  }

  try {
    const res = await fetch(url, init);
    const body = await res.text();
    return new Response(body, {
      status: res.status,
      headers: {
        "content-type": res.headers.get("content-type") ?? "application/json",
      },
    });
  } catch {
    return new Response(
      JSON.stringify({ error: "Care API is not reachable." }),
      { status: 502, headers: { "content-type": "application/json" } },
    );
  }
}

export { handler as GET, handler as POST };
