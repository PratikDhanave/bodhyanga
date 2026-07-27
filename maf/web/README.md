# Care console (web)

A [Next.js](https://nextjs.org) clinician console for the Bodh care API (`maf/cmd/care`).
Submit a case, poll status, list and answer physician reviews, and read a case's PHI-free
audit trail.

> **Not a medical device — research/education only, not for clinical use.**

## Run it

The console talks to the Go care API through a same-origin proxy (`app/api/care/[...path]`),
so start the API first:

```bash
# from maf/
go run ./cmd/care                 # care API on http://127.0.0.1:8088

# from maf/web/
npm install
npm run dev                       # console on http://localhost:3000
```

Override the API target with `CARE_API_URL` (default `http://127.0.0.1:8088` — the IPv4
literal, deliberately not `localhost`, since Node's fetch resolves `localhost` to IPv6 `::1`
first while `cmd/care` binds IPv4).

## Deployment — read before you ship

**Do not deploy this publicly as-is.** The care API wires **no authentication** (ADR-0006)
and handles clinical/PHI-bearing input. The console must sit **behind authenticated ingress**,
and the care API it proxies to must not be exposed. This is a research console, not a
production clinical product.
