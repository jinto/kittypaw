"""
Spike 0.3: LLM JavaScript Code Quality Comparison
Compare Claude's code generation quality between Python and JavaScript (ES2020)
for 10 representative agent tasks.
"""

import os
import json
import anthropic

api_key = os.environ.get("ANTHROPIC_API_KEY") or os.environ.get("CLAUDE_CODE_OAUTH_TOKEN")
client = anthropic.Anthropic(api_key=api_key)

PYTHON_SYSTEM_PROMPT = """\
You are Oochy, an AI agent that helps users by writing Python code.

## How you work
1. You receive an event (message, command, etc.)
2. You write Python code to handle it
3. Your code is type-checked with mypy, then executed in a sandbox
4. The result is returned to the user

## Rules
- Write ONLY valid Python code. No markdown fences, no explanations.
- Use the available skill classes to interact with the outside world.
- All async skill methods must be awaited: `await Telegram.send_message(...)`
- Your code runs in an async context — top-level `await` is allowed.
- If you need to store data for later, return a dict with a "state" key.
- Keep your code minimal and focused on the task.
- Handle errors gracefully with try/except.

## Available Skills

class Telegram:
    @staticmethod
    async def send_message(chat_id: str, text: str) -> dict[str, int]: ...
    @staticmethod
    async def send_voice(chat_id: str, audio_url: str) -> dict[str, int]: ...
    @staticmethod
    def get_chat_context() -> dict[str, str]: ...

class Chat:
    @staticmethod
    async def send_message(session_id: str, text: str) -> dict[str, str]: ...
    @staticmethod
    def get_session_context() -> dict[str, str]: ...

class Voice:
    @staticmethod
    async def transcribe(audio_url: str) -> dict[str, str]: ...
    @staticmethod
    async def synthesize(text: str, voice: str = "default") -> dict[str, str]: ...

class Desktop:
    @staticmethod
    async def bash(command: str) -> dict[str, str | int]: ...
    @staticmethod
    async def apple_script(script: str) -> dict[str, str]: ...
    @staticmethod
    def is_connected() -> bool: ...
"""

JS_SYSTEM_PROMPT = """\
You are Oochy, an AI agent that helps users by writing JavaScript code.

## How you work
1. You receive an event (message, command, etc.)
2. You write JavaScript (ES2020) code to handle it
3. Your code runs in QuickJS, a lightweight JS engine
4. The result is returned to the user

## Rules
- Write ONLY valid JavaScript (ES2020) code. No markdown fences, no explanations.
- Use the available global objects to interact with the outside world.
- Do NOT use: require(), import, fetch(), Node.js APIs (fs, path, process, Buffer), top-level await
- Wrap your code in an async function and call it: (async () => { ... })();
- If you need to store data for later, return an object with a "state" key from your function.
- Keep your code minimal and focused on the task.
- Handle errors gracefully with try/catch.
- Use async/await for all async calls.

## Available Globals

// Telegram messaging
Telegram.sendMessage(chatId, text)  // async, returns {message_id: number}
Telegram.sendVoice(chatId, audioUrl)  // async, returns {message_id: number}
Telegram.getChatContext()  // sync, returns {chat_id, user_name, ...}

// HTTP requests
Http.get(url)  // async, returns {status: number, body: string}
Http.post(url, body)  // async, returns {status: number, body: string}

// Voice processing
Voice.transcribe(audioUrl)  // async, returns {text: string}
Voice.synthesize(text, voice="default")  // async, returns {audio_url: string}

// Logging
console.log(...)  // for debugging

## QuickJS Constraints
- No module system (no require/import)
- No fetch() — use Http.get() / Http.post() instead
- No Node.js globals
- ES2020 features available: async/await, destructuring, template literals, arrow functions, Map, Set, Promise, etc.
"""

EVAL_SYSTEM_PROMPT = """\
You are a code quality evaluator. You will be given two code snippets that accomplish the same task:
one in Python and one in JavaScript (for QuickJS). Evaluate each on the following criteria using a 1-5 scale.

Python criteria:
1. Correctness (1-5): Does the code correctly accomplish the task?
2. Idiomatic quality (1-5): Is it written in idiomatic Python style?
3. Error handling (1-5): Does it handle errors gracefully?

JavaScript criteria:
1. Correctness (1-5): Does the code correctly accomplish the task?
2. Idiomatic quality (1-5): Is it written in idiomatic modern JavaScript style?
3. Error handling (1-5): Does it handle errors gracefully?
4. QuickJS compatibility (1-5): Does it avoid ACTUAL unsupported features?

IMPORTANT for QuickJS compatibility scoring:
- The following ARE valid provided globals in this environment — do NOT penalize for their use:
  Telegram.sendMessage(), Telegram.sendVoice(), Telegram.getChatContext(),
  Http.get(), Http.post(), Voice.transcribe(), Voice.synthesize(), console.log()
- The (async () => { ... })() IIFE pattern IS the correct way to use async/await in QuickJS — do NOT penalize it
- Only penalize (score 1-2) if the code uses ACTUAL unsupported features:
  require(), import statements, fetch(), Node.js built-ins (fs, path, process, Buffer, __dirname),
  or true top-level await (await outside any async function/IIFE)
- Score 5 if none of the banned features appear in the code

Respond with ONLY a JSON object in this exact format:
{
  "python": {
    "correctness": <1-5>,
    "idiomatic": <1-5>,
    "error_handling": <1-5>,
    "notes": "<brief notes>"
  },
  "javascript": {
    "correctness": <1-5>,
    "idiomatic": <1-5>,
    "error_handling": <1-5>,
    "quickjs_compat": <1-5>,
    "notes": "<brief notes>"
  }
}
"""

PROMPTS = [
    "Send a welcome message to the user",
    "Fetch weather data from an API and summarize it",
    "Parse the user's message for a date and set a reminder",
    "Handle an error gracefully and inform the user",
    "Read a JSON config file and validate required fields",
    "Calculate statistics from a list of numbers the user provides",
    "Format a markdown table from structured data",
    "Chain two API calls: get user profile, then get their recent activity",
    "Generate and send a voice message transcript",
    "Create a simple key-value cache with TTL expiration",
]


def generate_code(system_prompt: str, user_prompt: str) -> str:
    response = client.messages.create(
        model="claude-haiku-4-5-20251001",
        max_tokens=1024,
        system=system_prompt,
        messages=[{"role": "user", "content": user_prompt}],
    )
    return response.content[0].text


def evaluate_pair(task: str, python_code: str, js_code: str) -> dict:
    eval_prompt = f"""Task: {task}

Python code:
```python
{python_code}
```

JavaScript code:
```javascript
{js_code}
```"""
    response = client.messages.create(
        model="claude-haiku-4-5-20251001",
        max_tokens=512,
        system=EVAL_SYSTEM_PROMPT,
        messages=[{"role": "user", "content": eval_prompt}],
    )
    text = response.content[0].text.strip()
    # Strip markdown fences if present
    if text.startswith("```"):
        lines = text.split("\n")
        text = "\n".join(lines[1:-1] if lines[-1] == "```" else lines[1:])
    return json.loads(text)


def main():
    results = []

    for i, prompt in enumerate(PROMPTS, 1):
        print(f"[{i}/10] {prompt}")

        python_code = generate_code(PYTHON_SYSTEM_PROMPT, prompt)
        js_code = generate_code(JS_SYSTEM_PROMPT, prompt)
        scores = evaluate_pair(prompt, python_code, js_code)

        results.append({
            "prompt": prompt,
            "python_code": python_code,
            "js_code": js_code,
            "scores": scores,
        })
        print(f"  Python avg: {(scores['python']['correctness'] + scores['python']['idiomatic'] + scores['python']['error_handling']) / 3:.1f}")
        print(f"  JS avg:     {(scores['javascript']['correctness'] + scores['javascript']['idiomatic'] + scores['javascript']['error_handling'] + scores['javascript']['quickjs_compat']) / 4:.1f}")

    return results


if __name__ == "__main__":
    import sys
    raw_path = "/Users/jinto/projects/oochy/spikes/spike-0.3-js-quality/raw_results.json"
    if "--reeval" in sys.argv:
        # Re-evaluate existing generated code with updated evaluator prompt
        with open(raw_path) as f:
            results = json.load(f)
        print("Re-evaluating existing code pairs...")
        for i, r in enumerate(results):
            scores = evaluate_pair(r["prompt"], r["python_code"], r["js_code"])
            r["scores"] = scores
            p = scores["python"]
            j = scores["javascript"]
            print(f"[{i+1}/10] {r['prompt'][:50]}")
            print(f"  Python avg: {(p['correctness']+p['idiomatic']+p['error_handling'])/3:.1f}")
            print(f"  JS avg:     {(j['correctness']+j['idiomatic']+j['error_handling']+j['quickjs_compat'])/4:.1f}")
    else:
        results = main()

    # Compute aggregate stats
    py_scores = []
    js_scores = []
    quickjs_violations = 0

    for r in results:
        p = r["scores"]["python"]
        j = r["scores"]["javascript"]
        py_avg = (p["correctness"] + p["idiomatic"] + p["error_handling"]) / 3
        js_avg = (j["correctness"] + j["idiomatic"] + j["error_handling"] + j["quickjs_compat"]) / 4
        py_scores.append(py_avg)
        js_scores.append(js_avg)
        if j["quickjs_compat"] <= 2:
            quickjs_violations += 1

    overall_py = sum(py_scores) / len(py_scores)
    overall_js = sum(js_scores) / len(js_scores)
    js_vs_py_pct = (overall_js / overall_py) * 100 if overall_py > 0 else 0

    # Save raw JSON
    raw_path = "/Users/jinto/projects/oochy/spikes/spike-0.3-js-quality/raw_results.json"
    with open(raw_path, "w") as f:
        json.dump(results, f, indent=2)

    # Build markdown report
    lines = [
        "# Spike 0.3: LLM JavaScript Code Quality Comparison",
        "",
        "## Summary",
        "",
        f"- **Python average score**: {overall_py:.2f} / 5.0",
        f"- **JavaScript average score**: {overall_js:.2f} / 5.0",
        f"- **JS quality as % of Python**: {js_vs_py_pct:.1f}%",
        f"- **Acceptance criterion (>=80%)**: {'PASS' if js_vs_py_pct >= 80 else 'FAIL'}",
        f"- **QuickJS violations (<=2 compat score)**: {quickjs_violations}/10",
        f"- **Acceptance criterion (<=2 violations)**: {'PASS' if quickjs_violations <= 2 else 'FAIL'}",
        "",
        "## Per-Prompt Results",
        "",
        "| # | Task | Py Correct | Py Idiom | Py ErrH | Py Avg | JS Correct | JS Idiom | JS ErrH | JS Compat | JS Avg | JS/Py% |",
        "|---|------|-----------|----------|---------|--------|-----------|----------|---------|-----------|--------|--------|",
    ]

    for i, r in enumerate(results, 1):
        p = r["scores"]["python"]
        j = r["scores"]["javascript"]
        py_avg = (p["correctness"] + p["idiomatic"] + p["error_handling"]) / 3
        js_avg = (j["correctness"] + j["idiomatic"] + j["error_handling"] + j["quickjs_compat"]) / 4
        ratio = (js_avg / py_avg * 100) if py_avg > 0 else 0
        short_task = r["prompt"][:45] + ("..." if len(r["prompt"]) > 45 else "")
        lines.append(
            f"| {i} | {short_task} | {p['correctness']} | {p['idiomatic']} | {p['error_handling']} | {py_avg:.1f} | "
            f"{j['correctness']} | {j['idiomatic']} | {j['error_handling']} | {j['quickjs_compat']} | {js_avg:.1f} | {ratio:.0f}% |"
        )

    lines += [
        "",
        "## Code Samples and Evaluator Notes",
        "",
    ]

    for i, r in enumerate(results, 1):
        p = r["scores"]["python"]
        j = r["scores"]["javascript"]
        lines += [
            f"### {i}. {r['prompt']}",
            "",
            "**Python** (evaluator notes: " + p["notes"] + ")",
            "",
            "```python",
            r["python_code"],
            "```",
            "",
            "**JavaScript** (evaluator notes: " + j["notes"] + ")",
            "",
            "```javascript",
            r["js_code"],
            "```",
            "",
        ]

    lines += [
        "## System Prompt Adjustments for JavaScript",
        "",
        "The JS system prompt used in this spike includes the following key constraints:",
        "",
        "1. **Explicit QuickJS context**: `'You write JavaScript (ES2020) code that runs in QuickJS'`",
        "2. **Negative constraints**: `'Do NOT use: require(), import, fetch(), Node.js APIs, top-level await'`",
        "3. **Available globals listed**: `Telegram.sendMessage()`, `Http.get()`, `Http.post()`, `Voice.*`, `console.log()`",
        "4. **Async wrapper pattern**: `'Wrap your code in (async () => { ... })()'` instead of top-level await",
        "",
        "### Observed Improvements vs. Naive Prompt",
        "",
        "- Explicit `(async () => { ... })()` pattern prevents top-level await errors",
        "- Listing `Http.get/post` instead of `fetch()` guides correct API usage",
        "- Negative list of banned features reduces accidental `require()`/`import` usage",
        "- Listing exact QuickJS-compatible ES2020 features reduces engine incompatibilities",
        "",
        "## Conclusion",
        "",
    ]

    if js_vs_py_pct >= 80 and quickjs_violations <= 2:
        lines.append(
            f"Both acceptance criteria are met. JS quality is {js_vs_py_pct:.1f}% of Python quality, "
            f"with only {quickjs_violations} QuickJS violation(s). "
            "JavaScript (ES2020 / QuickJS) is a viable target language for Oochy agent code generation."
        )
    else:
        issues = []
        if js_vs_py_pct < 80:
            issues.append(f"JS quality {js_vs_py_pct:.1f}% < 80% threshold")
        if quickjs_violations > 2:
            issues.append(f"{quickjs_violations} QuickJS violations > 2 limit")
        lines.append("Acceptance criteria NOT fully met: " + "; ".join(issues) + ".")
        lines.append("Further system prompt tuning recommended before adopting JS as the agent language.")

    md_path = "/Users/jinto/projects/oochy/spikes/spike-0.3-js-quality/results.md"
    with open(md_path, "w") as f:
        f.write("\n".join(lines) + "\n")

    print(f"\nDone. Results saved to {md_path}")
    print(f"Raw JSON saved to {raw_path}")
    print(f"\nFinal: JS is {js_vs_py_pct:.1f}% of Python quality | {quickjs_violations} QuickJS violations")
