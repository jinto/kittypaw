# /// script
# requires-python = ">=3.11"
# dependencies = ["websockets", "httpx"]
# ///
"""
WS concurrent connection stress test for kittypaw-relay.

Ramps up WebSocket connections in steps, measuring connection latency
and polling the server's own /admin/stats for accurate metrics.

Usage:
    uv run bench/ws_flood.py [--host HOST] [--steps 100,500,1000,2000,5000]
"""
import argparse
import asyncio
import os
import statistics
import time

import httpx
import websockets


async def register(client: httpx.AsyncClient, base_url: str) -> str:
    r = await client.post(f"{base_url}/register")
    r.raise_for_status()
    return r.json()["token"]


async def open_ws(url: str, latencies: list, errors: list, conns: list):
    t0 = time.monotonic()
    try:
        ws = await websockets.connect(url, open_timeout=10)
        latencies.append(time.monotonic() - t0)
        conns.append(ws)
    except Exception as e:
        errors.append(str(e))


async def server_stats(client: httpx.AsyncClient, base_url: str, secret: str) -> dict | None:
    try:
        r = await client.get(f"{base_url}/admin/stats", params={"secret": secret})
        if r.status_code == 200:
            return r.json()
    except Exception:
        pass
    return None


def print_server_stats(label: str, stats: dict | None):
    if stats is None:
        print(f"  Server [{label}]  (unavailable)")
        return
    rss_mb = stats.get("rss_bytes", 0) / 1024 / 1024
    print(f"  Server [{label}]  WS={stats.get('ws_sessions', '?')}"
          f"  RSS={rss_mb:.1f}MB"
          f"  FDs={stats.get('fd_count', '?')}")


async def run_step(
    base_url: str,
    ws_base: str,
    count: int,
    existing_conns: list,
    client: httpx.AsyncClient,
    secret: str,
):
    print(f"\n{'='*60}")
    print(f"  Step: +{count} connections (total target: {len(existing_conns) + count})")
    print(f"{'='*60}")

    before = await server_stats(client, base_url, secret)
    print_server_stats("before", before)

    # Phase 1: Register tokens
    t0 = time.monotonic()
    tokens = await asyncio.gather(*[register(client, base_url) for _ in range(count)])
    reg_time = time.monotonic() - t0
    print(f"  Register: {count} tokens in {reg_time:.2f}s ({count/reg_time:.0f} req/s)")

    # Phase 2: Open WS connections
    latencies = []
    errors = []
    new_conns = []

    t0 = time.monotonic()
    await asyncio.gather(
        *[open_ws(f"{ws_base}/ws/{t}", latencies, errors, new_conns) for t in tokens]
    )
    wall_time = time.monotonic() - t0

    existing_conns.extend(new_conns)

    ok = len(latencies)
    fail = len(errors)

    after = await server_stats(client, base_url, secret)

    print(f"  Connected: {ok}  Failed: {fail}  Wall time: {wall_time:.2f}s")
    print(f"  Total alive: {len(existing_conns)}")
    print_server_stats("after", after)

    if before and after:
        delta = (after.get("rss_bytes", 0) - before.get("rss_bytes", 0)) / 1024 / 1024
        print(f"  RSS delta: {'+' if delta >= 0 else ''}{delta:.1f}MB")

    if latencies:
        latencies.sort()
        n = len(latencies)
        print(f"  Latency  p50={latencies[n//2]*1000:.1f}ms"
              f"  p95={latencies[int(n*0.95)]*1000:.1f}ms"
              f"  p99={latencies[int(n*0.99)]*1000:.1f}ms"
              f"  max={latencies[-1]*1000:.1f}ms")
        if n > 1:
            print(f"  Mean={statistics.mean(latencies)*1000:.1f}ms"
                  f"  Stdev={statistics.stdev(latencies)*1000:.1f}ms")

    if errors:
        unique = set(errors)
        print(f"  Error types ({len(unique)}):")
        for e in list(unique)[:5]:
            print(f"    - {e}")

    return fail


async def close_all(conns: list):
    print(f"\nClosing {len(conns)} connections...")
    await asyncio.gather(*[ws.close() for ws in conns], return_exceptions=True)
    print("Done.")


async def main():
    parser = argparse.ArgumentParser(description="WS flood test for kittypaw-relay")
    parser.add_argument("--host", default="localhost:9088")
    parser.add_argument("--secret", default=os.environ.get("WEBHOOK_SECRET", ""),
                        help="WEBHOOK_SECRET (default: $WEBHOOK_SECRET env var)")
    parser.add_argument("--steps", default="100,500,1000,2000,5000",
                        help="Comma-separated connection counts per step")
    parser.add_argument("--hold", type=float, default=3.0,
                        help="Seconds to hold connections between steps")
    args = parser.parse_args()

    base_url = f"http://{args.host}"
    ws_base = f"ws://{args.host}"
    steps = [int(s) for s in args.steps.split(",")]

    if not args.secret:
        print("WARNING: --secret not set — server metrics will be unavailable")

    print(f"Target: {base_url}")
    print(f"Steps: {steps}")
    print(f"Hold: {args.hold}s between steps\n")

    all_conns = []

    async with httpx.AsyncClient(timeout=30) as client:
        # Verify server stats are accessible
        initial = await server_stats(client, base_url, args.secret)
        if initial:
            print(f"Server stats OK: {initial}")
        else:
            print("Could not fetch /admin/stats — check --secret")

        for step_count in steps:
            failures = await run_step(
                base_url, ws_base, step_count, all_conns, client, args.secret
            )

            print(f"  Holding {len(all_conns)} connections for {args.hold}s...")
            await asyncio.sleep(args.hold)

            alive = sum(1 for ws in all_conns if ws.close_code is None)
            dropped = len(all_conns) - alive
            if dropped:
                print(f"  ** {dropped} connections dropped during hold! **")
                all_conns = [ws for ws in all_conns if ws.close_code is None]

            if failures > step_count * 0.2:
                print(f"\n  >20% failures — ceiling near {len(all_conns)} connections.")
                break

    async with httpx.AsyncClient(timeout=30) as final_client:
        final = await server_stats(final_client, base_url, args.secret)
    print(f"\n{'='*60}")
    print(f"  RESULT: {len(all_conns)} live connections held")
    print_server_stats("final", final)
    print(f"{'='*60}")

    await close_all(all_conns)


if __name__ == "__main__":
    asyncio.run(main())
