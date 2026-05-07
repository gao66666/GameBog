"""
Agent 测试脚本 — 直接调 /chat 并打印 SSE 事件
用法: python test_agent.py "有什么热门文章"
      python test_agent.py "我的积分多少" --token=xxx
"""
import argparse
import json
import urllib.request

AGENT_URL = "http://127.0.0.1:9091/chat"


def chat(message: str, token: str = ""):
    body = json.dumps({"message": message, "token": token}).encode()
    req = urllib.request.Request(
        AGENT_URL,
        data=body,
        headers={"Content-Type": "application/json", "Accept": "text/event-stream"},
    )

    with urllib.request.urlopen(req, timeout=120) as resp:
        for line in resp:
            line = line.decode().strip()
            if not line.startswith("data: "):
                continue
            data = json.loads(line[6:])
            t = data.get("type", "?")

            if t == "rewrite":
                print(f"[改写] {data['original']} → {data['rewritten']} ({data['rewrite_ms']}ms)")
            elif t == "route":
                print(f"[路由] servers={data['servers']} ({data['route_ms']}ms)")
            elif t == "token":
                print(data["content"], end="", flush=True)
            elif t == "tool_start":
                print(f"\n  🔧 {data['tool']}", end=" ")
            elif t == "tool_end":
                print(f"→ {data['preview']}")
            elif t == "error":
                print(f"\n❌ {data.get('content', 'unknown')}")
            elif t == "done":
                print()
        print()


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("message", help="要问的问题")
    parser.add_argument("--token", default="", help="用户 JWT token")
    args = parser.parse_args()
    chat(args.message, args.token)
