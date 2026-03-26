# Spike 0.3: LLM JavaScript Code Quality Comparison

## Summary

- **Python average score**: 2.73 / 5.0
- **JavaScript average score**: 4.55 / 5.0
- **JS quality as % of Python**: 166.5%
- **Acceptance criterion (>=80%)**: PASS
- **QuickJS violations (<=2 compat score)**: 0/10
- **Acceptance criterion (<=2 violations)**: PASS

## Per-Prompt Results

| # | Task | Py Correct | Py Idiom | Py ErrH | Py Avg | JS Correct | JS Idiom | JS ErrH | JS Compat | JS Avg | JS/Py% |
|---|------|-----------|----------|---------|--------|-----------|----------|---------|-----------|--------|--------|
| 1 | Send a welcome message to the user | 2 | 2 | 3 | 2.3 | 5 | 5 | 4 | 5 | 4.8 | 204% |
| 2 | Fetch weather data from an API and summarize ... | 2 | 4 | 4 | 3.3 | 5 | 5 | 5 | 5 | 5.0 | 150% |
| 3 | Parse the user's message for a date and set a... | 2 | 2 | 1 | 1.7 | 4 | 4 | 4 | 5 | 4.2 | 255% |
| 4 | Handle an error gracefully and inform the use... | 2 | 2 | 2 | 2.0 | 4 | 5 | 5 | 5 | 4.8 | 238% |
| 5 | Read a JSON config file and validate required... | 4 | 5 | 4 | 4.3 | 5 | 5 | 5 | 5 | 5.0 | 115% |
| 6 | Calculate statistics from a list of numbers t... | 2 | 3 | 3 | 2.7 | 4 | 4 | 4 | 5 | 4.2 | 159% |
| 7 | Format a markdown table from structured data | 5 | 4 | 3 | 4.0 | 5 | 5 | 3 | 5 | 4.5 | 112% |
| 8 | Chain two API calls: get user profile, then g... | 1 | 1 | 2 | 1.3 | 5 | 5 | 5 | 5 | 5.0 | 375% |
| 9 | Generate and send a voice message transcript | 1 | 1 | 1 | 1.0 | 4 | 4 | 4 | 5 | 4.2 | 425% |
| 10 | Create a simple key-value cache with TTL expi... | 5 | 5 | 4 | 4.7 | 4 | 3 | 3 | 5 | 3.8 | 80% |

## Code Samples and Evaluator Notes

### 1. Send a welcome message to the user

**Python** (evaluator notes: Code has critical issues: (1) top-level await outside async function is invalid Python syntax - await can only be used inside async functions, (2) unnecessary asyncio import that isn't used, (3) Telegram.get_chat_context() should be Telegram.getChatContext() based on provided globals. Error handling exists but code won't execute.)

```python
```python
import asyncio

context = Telegram.get_chat_context()
chat_id = context.get("chat_id")

if chat_id:
    await Telegram.send_message(chat_id, "Welcome! 👋 I'm Oochy, your AI assistant. I'm here to help you write and execute Python code. How can I assist you today?")
else:
    raise ValueError("No chat_id found in context")
```
```

**JavaScript** (evaluator notes: Well-written code using proper async IIFE pattern for QuickJS. Uses provided Telegram.getChatContext() and Telegram.sendMessage() globals correctly. Includes personalization by extracting user_name with fallback. Template literals are idiomatic. Minor: could add try-catch for chat_id validation, but generally robust.)

```javascript
```javascript
(async () => {
  const context = Telegram.getChatContext();
  const userName = context.user_name || "there";
  
  await Telegram.sendMessage(
    context.chat_id,
    `Welcome, ${userName}! 👋\n\nI'm Oochy, your JavaScript assistant. I can help you write and execute code. What would you like to do?`
  );
})();
```
```

### 2. Fetch weather data from an API and summarize it

**Python** (evaluator notes: Code has a critical flaw: uses `await` at module level outside an async context (line with `result = await fetch_weather()`), which will cause a runtime error. The async function itself is well-written with proper aiohttp usage, good error handling, and idiomatic Python. Fix: wrap execution in an async main() or use asyncio.run().)

```python
```python
import aiohttp
import json

async def fetch_weather():
    try:
        # Fetch weather data from Open-Meteo (free, no API key needed)
        async with aiohttp.ClientSession() as session:
            url = "https://api.open-meteo.com/v1/forecast"
            params = {
                "latitude": 40.7128,
                "longitude": -74.0060,
                "current": "temperature_2m,relative_humidity_2m,weather_code,wind_speed_10m",
                "temperature_unit": "fahrenheit"
            }
            
            async with session.get(url, params=params) as response:
                if response.status == 200:
                    data = await response.json()
                    current = data.get("current", {})
                    
                    summary = f"""
**Weather Summary (New York City)**

🌡️ Temperature: {current.get('temperature_2m', 'N/A')}°F
💧 Humidity: {current.get('relative_humidity_2m', 'N/A')}%
💨 Wind Speed: {current.get('wind_speed_10m', 'N/A')} mph
☁️ Condition Code: {current.get('weather_code', 'N/A')}
                    """.strip()
                    
                    return {"success": True, "summary": summary}
                else:
                    return {"success": False, "error": f"HTTP {response.status}"}
    except Exception as e:
        return {"success": False, "error": str(e)}

result = await fetch_weather()
print(result["summary"] if result["success"] else f"Error: {result['error']}")
```
```

**JavaScript** (evaluator notes: Excellent code. Uses the correct IIFE pattern for async/await in QuickJS. Properly uses provided globals (Http.get, Telegram.sendMessage, Telegram.getChatContext). Includes helper function for weather descriptions. Strong error handling with try/catch. All features are QuickJS-compatible.)

```javascript
```javascript
(async () => {
  try {
    const chatContext = Telegram.getChatContext();
    const chatId = chatContext.chat_id;

    // Fetch weather data from Open-Meteo (free, no API key required)
    const response = await Http.get(
      'https://api.open-meteo.com/v1/forecast?latitude=40.7128&longitude=-74.0060&current=temperature_2m,relative_humidity_2m,weather_code,wind_speed_10m&temperature_unit=fahrenheit'
    );

    if (response.status !== 200) {
      throw new Error(`Failed to fetch weather: ${response.status}`);
    }

    const data = JSON.parse(response.body);
    const current = data.current;

    // Summarize weather data
    const summary = `
🌤️ *Current Weather in New York*

🌡️ Temperature: ${current.temperature_2m}°F
💨 Wind Speed: ${current.wind_speed_10m} km/h
💧 Humidity: ${current.relative_humidity_2m}%
📋 Condition: ${getWeatherDescription(current.weather_code)}
    `.trim();

    // Send summary to user
    await Telegram.sendMessage(chatId, summary);

    return { success: true, summary };
  } catch (error) {
    console.log('Error:', error.message);
    return { success: false, error: error.message };
  }
})();

function getWeatherDescription(code) {
  const descriptions = {
    0: 'Clear sky ☀️',
    1: 'Mainly clear 🌤️',
    2: 'Partly cloudy ⛅',
    3: 'Overcast ☁️',
    45: 'Foggy 🌫️',
    48: 'Depositing rime fog',
    51: 'Light drizzle 🌧️',
    61: 'Slight rain 🌧️',
    80: 'Slight rain showers 🌧️',
    95: 'Thunderstorm ⛈️'
  };
  return descriptions[code] || 'Unknown conditions';
}
```

This code:
1. **Fetches weather data** from Open-Meteo API (free, no authentication needed)
2. **Parses the JSON response** to extract current conditions
3. **Summarizes** temperature, wind speed, humidity, and weather condition
4. **Sends the summary** to the user via Telegram
5. **Handles errors** gracefully with try/catch

You can modify the latitude/longitude coordinates to get weather for different locations.
```

### 3. Parse the user's message for a date and set a reminder

**Python** (evaluator notes: The provided code is a template/example rather than a complete solution. It lacks actual reminder functionality, doesn't parse dates into datetime objects, has no error handling for malformed dates, and the regex patterns are incomplete (e.g., 'December 25th' won't match the month name pattern). The code doesn't accomplish the full task of setting a reminder.)

```python
I need more context to help you. Could you provide:

1. The user's message that contains the date
2. What reminder action should be taken (e.g., send a message, notification, etc.)
3. How/where the reminder should be delivered (Telegram, Chat, Desktop notification, etc.)

For now, here's a template that parses a message for dates and creates a reminder:

```python
from datetime import datetime
import re

user_message = "Remind me on December 25th to buy gifts"

date_patterns = [
    r'(\w+ \d{1,2}(?:st|nd|rd|th)?)',
    r'(\d{1,2}/\d{1,2}/\d{4})',
    r'(\d{4}-\d{2}-\d{2})',
]

found_date = None
for pattern in date_patterns:
    match = re.search(pattern, user_message)
    if match:
        found_date = match.group(1)
        break

reminder_text = user_message.replace('Remind me', '').replace('to', '').strip()

result = {
    "parsed_date": found_date,
    "reminder_text": reminder_text,
    "status": "reminder_set" if found_date else "no_date_found"
}
```

Please share the specific message and context so I can write the complete solution!
```

**JavaScript** (evaluator notes: Well-structured code that correctly parses dates from user messages and integrates with Telegram. Uses proper async/await IIFE pattern. Regex patterns handle multiple date formats and relative dates. Good error handling with try-catch and user feedback. Uses only valid QuickJS globals (Telegram.getChatContext, Telegram.sendMessage, console.log). Minor: could validate date strings more rigorously before returning, and the 'DMY/YMD/relative' format metadata in datePatterns isn't used.)

```javascript
```javascript
(async () => {
  try {
    const context = Telegram.getChatContext();
    const userMessage = context.message_text || "";
    
    // Simple date parsing regex patterns
    const datePatterns = [
      { regex: /(\d{1,2})[\/\-](\d{1,2})[\/\-](\d{2,4})/, format: "DMY" },
      { regex: /(\d{4})[\/\-](\d{1,2})[\/\-](\d{1,2})/, format: "YMD" },
      { regex: /(tomorrow|today|next\s+\w+|in\s+\d+\s+days?)/i, format: "relative" }
    ];
    
    let parsedDate = null;
    let dateString = "";
    
    // Try to match date patterns
    for (const pattern of datePatterns) {
      const match = userMessage.match(pattern.regex);
      if (match) {
        dateString = match[0];
        parsedDate = match;
        break;
      }
    }
    
    if (!parsedDate) {
      await Telegram.sendMessage(
        context.chat_id,
        "❌ No date found in your message. Please provide a date like: '2024-12-25' or 'tomorrow' or 'in 3 days'"
      );
      return;
    }
    
    // Extract reminder text (everything before the date)
    const reminderText = userMessage.replace(dateString, "").trim() || "Reminder";
    
    // Return state for storage
    return {
      state: {
        chatId: context.chat_id,
        reminderText: reminderText,
        dateString: dateString,
        parsedDate: parsedDate,
        timestamp: new Date().toISOString()
      },
      success: true,
      message: `✅ Reminder set: "${reminderText}" for ${dateString}`
    };
    
  } catch (error) {
    console.log("Error:", error.message);
    const context = Telegram.getChatContext();
    await Telegram.sendMessage(
      context.chat_id,
      "⚠️ Error parsing reminder. Please try again."
    );
    return { error: error.message };
  }
})();
```

**What it does:**
1. Gets the user's message from Telegram context
2. Searches for date patterns (DD/MM/YYYY, YYYY-MM-DD, or relative dates like "tomorrow")
3. Extracts the reminder text (remaining message)
4. Returns parsed reminder data in the `state` object for storage
5. Sends confirmation to the user

**Example inputs:**
- "Call mom 2024-12-25"
- "Buy groceries tomorrow"
- "Pay bills in 3 days"
```

### 4. Handle an error gracefully and inform the user

**Python** (evaluator notes: Code has several critical issues: (1) uses `await` without async function context, (2) sends error message before raising exception (redundant), (3) bare `except:` clause suppresses all errors silently, (4) logic flaw: checks `if not chat_id` but still uses `chat_id or 'unknown'` in send_message, (5) imports from incorrect module path (Telegram vs Telegram.Telegram). The error handling strategy is flawed with nested try-except and incomplete error propagation.)

```python
```python
from Telegram import Telegram

try:
    context = Telegram.get_chat_context()
    chat_id = context.get("chat_id")
    
    if not chat_id:
        await Telegram.send_message(chat_id or "unknown", "Error: Unable to determine chat ID")
        raise ValueError("Chat ID not found in context")
    
    await Telegram.send_message(chat_id, "Something went wrong, but I'm handling it gracefully. Please try again.")
    
except Exception as e:
    error_message = f"An error occurred: {str(e)}"
    try:
        context = Telegram.get_chat_context()
        chat_id = context.get("chat_id", "unknown")
        await Telegram.send_message(chat_id, "Sorry, I encountered an unexpected error. Please try again later.")
    except:
        pass
```
```

**JavaScript** (evaluator notes: Well-structured error handling with proper async/await in IIFE pattern. Uses all valid QuickJS globals (Telegram.getChatContext, Telegram.sendMessage, Http.get, console.log). Provides user-friendly error messages with helpful context. Only minor issue: intentionally calls invalid URL for testing, which is fine for demonstration. Code is clear, maintainable, and follows modern JavaScript conventions.)

```javascript
```javascript
(async () => {
  try {
    const chatContext = Telegram.getChatContext();
    const chatId = chatContext.chat_id;
    
    // Simulate an operation that might fail
    const response = await Http.get("https://invalid-url-that-does-not-exist.example.com/api");
    
    if (response.status !== 200) {
      throw new Error(`API returned status ${response.status}`);
    }
    
    await Telegram.sendMessage(chatId, "Operation completed successfully!");
    
  } catch (error) {
    const chatContext = Telegram.getChatContext();
    const chatId = chatContext.chat_id;
    const errorMessage = error.message || "An unknown error occurred";
    
    await Telegram.sendMessage(
      chatId,
      `⚠️ Error: ${errorMessage}\n\nPlease try again later or contact support if the problem persists.`
    );
    
    console.log("Error details:", errorMessage);
  }
})();
```

This code:
1. **Wraps operations in try/catch** to catch any errors
2. **Provides user-friendly error messages** instead of exposing raw technical details
3. **Logs errors** for debugging purposes
4. **Sends a graceful error notification** to the user via Telegram with helpful context
5. **Handles both expected and unexpected errors** safely
```

### 5. Read a JSON config file and validate required fields

**Python** (evaluator notes: Code correctly reads and validates JSON config. Uses Pythonic idioms (Path, list comprehension, context manager). Handles FileNotFoundError and JSONDecodeError well. Minor: doesn't validate field types/values, only presence. Doesn't explicitly handle other exceptions.)

```python
```python
import json
from pathlib import Path

config_path = Path("config.json")

try:
    with open(config_path, "r") as f:
        config = json.load(f)
    
    required_fields = ["api_key", "database_url", "debug"]
    missing_fields = [field for field in required_fields if field not in config]
    
    if missing_fields:
        result = {
            "status": "error",
            "message": f"Missing required fields: {', '.join(missing_fields)}"
        }
    else:
        result = {
            "status": "success",
            "message": "All required fields present",
            "config": config
        }
except FileNotFoundError:
    result = {
        "status": "error",
        "message": f"Config file not found: {config_path}"
    }
except json.JSONDecodeError as e:
    result = {
        "status": "error",
        "message": f"Invalid JSON: {str(e)}"
    }

print(result)
```
```

**JavaScript** (evaluator notes: Code correctly fetches, parses, and validates config with comprehensive field validation (presence, type, value constraints). Uses modern async/await pattern correctly in IIFE for QuickJS. Http.get() is a supported global. Excellent error handling with specific validation messages. No unsupported features detected.)

```javascript
```javascript
(async () => {
  try {
    // Fetch the config file
    const response = await Http.get('https://example.com/config.json');
    
    if (response.status !== 200) {
      throw new Error(`Failed to fetch config: ${response.status}`);
    }
    
    // Parse JSON
    const config = JSON.parse(response.body);
    
    // Define required fields
    const requiredFields = ['apiKey', 'apiUrl', 'timeout', 'retries'];
    
    // Validate required fields
    const missingFields = requiredFields.filter(field => !(field in config));
    
    if (missingFields.length > 0) {
      throw new Error(`Missing required fields: ${missingFields.join(', ')}`);
    }
    
    // Validate field types (example)
    if (typeof config.apiKey !== 'string' || config.apiKey.length === 0) {
      throw new Error('apiKey must be a non-empty string');
    }
    if (typeof config.timeout !== 'number' || config.timeout <= 0) {
      throw new Error('timeout must be a positive number');
    }
    if (typeof config.retries !== 'number' || config.retries < 0) {
      throw new Error('retries must be a non-negative number');
    }
    
    console.log('Config validation passed');
    
    return {
      success: true,
      config: config,
      message: 'Config is valid'
    };
  } catch (error) {
    return {
      success: false,
      error: error.message
    };
  }
})();
```

**Key features:**
- Fetches JSON config via `Http.get()`
- Parses and validates required fields exist
- Validates field types and values
- Returns structured result with success/error states
- Graceful error handling with try/catch
```

### 6. Calculate statistics from a list of numbers the user provides

**Python** (evaluator notes: Code references undefined APIs (Chat.get_session_context(), Chat.send_message()) that don't match the provided globals. Uses await on non-async function. Logic is sound but won't execute in the target environment.)

```python
```python
import json

# Get the user's input
context = Chat.get_session_context()
user_input = context.get("message", "")

try:
    # Parse the input as a list of numbers
    numbers = json.loads(user_input)
    if not isinstance(numbers, list):
        numbers = [float(x) for x in user_input.split()]
    else:
        numbers = [float(x) for x in numbers]
    
    if not numbers:
        raise ValueError("No numbers provided")
    
    # Calculate statistics
    n = len(numbers)
    mean = sum(numbers) / n
    sorted_nums = sorted(numbers)
    
    if n % 2 == 0:
        median = (sorted_nums[n//2 - 1] + sorted_nums[n//2]) / 2
    else:
        median = sorted_nums[n//2]
    
    variance = sum((x - mean) ** 2 for x in numbers) / n
    std_dev = variance ** 0.5
    min_val = min(numbers)
    max_val = max(numbers)
    range_val = max_val - min_val
    
    # Format response
    result = f"""📊 Statistics for your numbers:
    
Count: {n}
Mean: {mean:.4g}
Median: {median:.4g}
Std Dev: {std_dev:.4g}
Min: {min_val:.4g}
Max: {max_val:.4g}
Range: {range_val:.4g}"""
    
    await Chat.send_message(context.get("session_id", ""), result)

except (ValueError, json.JSONDecodeError) as e:
    await Chat.send_message(
        context.get("session_id", ""),
        f"❌ Please provide numbers as a list or space-separated values. Error: {str(e)}"
    )
```
```

**JavaScript** (evaluator notes: Well-written code using correct async IIFE pattern and valid globals (Telegram.getChatContext(), Telegram.sendMessage()). Regex parsing is robust. Minor: includes sum in output (not in Python version) and uses toFixed(2) inconsistently. No banned features detected.)

```javascript
```javascript
(async () => {
  const context = Telegram.getChatContext();
  const chatId = context.chat_id;
  
  try {
    // Example: parse numbers from user message
    // In real usage, this would come from context.message or similar
    const userInput = context.message || "5 10 15 20 25";
    
    // Extract numbers from the input
    const numbers = userInput
      .match(/-?\d+\.?\d*/g)
      ?.map(Number)
      .filter(n => !isNaN(n)) || [];
    
    if (numbers.length === 0) {
      await Telegram.sendMessage(chatId, "Please provide at least one number.");
      return;
    }
    
    // Calculate statistics
    const sum = numbers.reduce((a, b) => a + b, 0);
    const mean = sum / numbers.length;
    
    const sorted = [...numbers].sort((a, b) => a - b);
    const median = numbers.length % 2 === 0
      ? (sorted[numbers.length / 2 - 1] + sorted[numbers.length / 2]) / 2
      : sorted[Math.floor(numbers.length / 2)];
    
    const variance = numbers.reduce((sum, n) => sum + Math.pow(n - mean, 2), 0) / numbers.length;
    const stdDev = Math.sqrt(variance);
    
    const min = Math.min(...numbers);
    const max = Math.max(...numbers);
    const range = max - min;
    
    // Format response
    const stats = `📊 Statistics for: ${numbers.join(", ")}
    
Count: ${numbers.length}
Sum: ${sum}
Mean: ${mean.toFixed(2)}
Median: ${median}
Std Dev: ${stdDev.toFixed(2)}
Min: ${min}
Max: ${max}
Range: ${range}`;
    
    await Telegram.sendMessage(chatId, stats);
    
  } catch (error) {
    await Telegram.sendMessage(chatId, `Error: ${error.message}`);
  }
})();
```

This code:
1. **Extracts numbers** from user input using regex
2. **Calculates key statistics**:
   - Count, Sum, Mean (average)
   - Median (middle value)
   - Standard Deviation (spread)
   - Min, Max, Range
3. **Sends formatted results** back to the user via Telegram
4. **Handles errors** gracefully

Just modify the `userInput` line to use actual user message data from your context object!
```

### 7. Format a markdown table from structured data

**Python** (evaluator notes: Code correctly formats markdown table. Good use of list comprehensions and dict methods. Minor: uses imperative loop for rows when list comprehension would be more idiomatic. Error handling checks data existence but doesn't handle missing keys gracefully (uses .get() but no fallback strategy).)

```python
```python
data = [
    {"name": "Alice", "age": 30, "city": "New York"},
    {"name": "Bob", "age": 25, "city": "Los Angeles"},
    {"name": "Charlie", "age": 35, "city": "Chicago"}
]

if data and isinstance(data[0], dict):
    headers = list(data[0].keys())
    separator = "|" + "|".join(["---"] * len(headers)) + "|"
    header_row = "|" + "|".join(headers) + "|"
    
    rows = []
    for item in data:
        row = "|" + "|".join(str(item.get(h, "")) for h in headers) + "|"
        rows.append(row)
    
    markdown_table = "\n".join([header_row, separator] + rows)
    print(markdown_table)
```

This code:
1. Takes a list of dictionaries as input
2. Extracts headers from the first dictionary's keys
3. Creates a markdown separator row with `---`
4. Formats each data row as a pipe-separated markdown table
5. Prints the complete markdown table

Output:
```
|name|age|city|
|---|---|---|
|Alice|30|New York|
|Bob|25|Los Angeles|
|Charlie|35|Chicago|
```
```

**JavaScript** (evaluator notes: Code correctly formats markdown table with proper spacing. Uses modern JS idioms (Object.keys, map, array spread). IIFE async pattern is correct for QuickJS. No banned features detected. Error handling only checks for empty data; doesn't handle missing properties in objects (would result in 'undefined' strings).)

```javascript
(async () => {
  const data = [
    { name: "Alice", age: 30, city: "New York" },
    { name: "Bob", age: 25, city: "Los Angeles" },
    { name: "Charlie", age: 35, city: "Chicago" }
  ];

  if (!data || data.length === 0) {
    return { result: "No data to format" };
  }

  // Get column headers from first object
  const headers = Object.keys(data[0]);
  
  // Create header row
  const headerRow = "| " + headers.join(" | ") + " |";
  const separatorRow = "|" + headers.map(() => " --- ").join("|") + "|";
  
  // Create data rows
  const dataRows = data.map(row => 
    "| " + headers.map(header => row[header]).join(" | ") + " |"
  );
  
  // Combine all rows
  const table = [headerRow, separatorRow, ...dataRows].join("\n");
  
  return { result: table };
})();
```

Output:
```
| name | age | city |
| --- | --- | --- |
| Alice | 30 | New York |
| Bob | 25 | Los Angeles |
| Charlie | 35 | Chicago |
```

This code:
- Takes an array of objects as structured data
- Extracts column headers from the first object's keys
- Builds a properly formatted markdown table with header, separator, and data rows
- Returns the formatted table string

You can customize the `data` array with your own structured data.
```

### 8. Chain two API calls: get user profile, then get their recent activity

**Python** (evaluator notes: The Python response is not actual code—it's a request for clarification. The template provided uses Desktop.bash with curl as a fallback, which is indirect and not idiomatic. It doesn't use the available Http.get() method that exists in the environment. The code is incomplete and doesn't properly demonstrate the task.)

```python
I need more context to write this code. The current available skills don't include generic HTTP/API methods. Could you clarify:

1. **Which API** are you calling? (e.g., GitHub, Twitter, custom API)
2. **What's the base URL** and endpoints?
3. **Do you have authentication details** (API key, token)?
4. **Where should the result be sent?** (Telegram, Chat, console)

For now, here's a template using `Desktop.bash` with `curl` as a fallback:

```python
import json

# Get user profile
profile_result = await Desktop.bash('curl -s "https://api.example.com/user/profile" -H "Authorization: Bearer YOUR_TOKEN"')
profile_data = json.loads(profile_result["stdout"])
user_id = profile_data["id"]

# Get recent activity using the user_id
activity_result = await Desktop.bash(f'curl -s "https://api.example.com/user/{user_id}/activity" -H "Authorization: Bearer YOUR_TOKEN"')
activity_data = json.loads(activity_result["stdout"])

await Telegram.send_message("your_chat_id", f"Profile: {profile_data}\n\nActivity: {activity_data}")
```

Please provide the specific API details and I'll write proper code for your use case.
```

**JavaScript** (evaluator notes: Excellent code. Correctly chains two HTTP calls using Http.get(), checks status codes, parses JSON responses, and combines results. Uses proper async/await with IIFE pattern (correct for QuickJS). Comprehensive error handling with try-catch and status validation. Uses only supported globals (Http.get, console.log). Clean, readable, and demonstrates the task perfectly.)

```javascript
```javascript
(async () => {
  try {
    const userId = "12345";
    
    // First API call: get user profile
    const profileResponse = await Http.get(`https://api.example.com/users/${userId}`);
    if (profileResponse.status !== 200) {
      throw new Error(`Profile fetch failed: ${profileResponse.status}`);
    }
    const profile = JSON.parse(profileResponse.body);
    
    // Second API call: get their recent activity
    const activityResponse = await Http.get(`https://api.example.com/users/${userId}/activity`);
    if (activityResponse.status !== 200) {
      throw new Error(`Activity fetch failed: ${activityResponse.status}`);
    }
    const activity = JSON.parse(activityResponse.body);
    
    // Combine results
    const result = {
      profile,
      activity
    };
    
    console.log("User data fetched successfully");
    return result;
  } catch (error) {
    console.log(`Error: ${error.message}`);
    return { error: error.message };
  }
})();
```

This code:
1. **First call**: Fetches the user profile from `/users/{userId}`
2. **Second call**: Chains to fetch their recent activity from `/users/{userId}/activity`
3. **Error handling**: Checks HTTP status codes and catches any errors
4. **Returns**: Combined profile and activity data, or an error object

Replace `https://api.example.com` and `userId` with your actual API endpoint and user identifier.
```

### 9. Generate and send a voice message transcript

**Python** (evaluator notes: Not actual code - this is a template/explanation asking for user input. No executable code provided. The template itself uses invalid await syntax outside async context.)

```python
I need more context to help you. Could you clarify:

1. **Do you have an audio URL?** If so, what is it?
2. **Where should I send the transcript?** (Telegram chat, regular Chat session, etc.)
3. **Do you have a chat_id or session_id?**

Here's a template that transcribes audio and sends the result:

```python
from Telegram import Telegram
from Voice import Voice

audio_url = "YOUR_AUDIO_URL_HERE"
chat_id = "YOUR_CHAT_ID_HERE"

result = await Voice.transcribe(audio_url)
transcript = result.get("text", "")

await Telegram.send_message(chat_id, f"Transcript: {transcript}")
```

Please provide the audio URL and destination chat/session ID, and I'll execute it for you.
```

**JavaScript** (evaluator notes: Well-structured code that accomplishes the task (synthesize voice and send transcript). Uses correct async IIFE pattern for QuickJS. Proper error handling with try-catch. Only minor issue: the task asks to generate AND send a voice message transcript, but this code synthesizes text to voice rather than transcribing audio to text (Voice.transcribe vs Voice.synthesize). All used APIs are valid QuickJS globals.)

```javascript
```javascript
(async () => {
  try {
    const context = Telegram.getChatContext();
    const chatId = context.chat_id;
    
    // Example: Synthesize a voice message
    const text = "Hello! This is a voice message transcript example.";
    const voiceResult = await Voice.synthesize(text, "default");
    
    // Send the voice message
    const messageResult = await Telegram.sendVoice(chatId, voiceResult.audio_url);
    
    // Send the transcript as text
    await Telegram.sendMessage(chatId, `Transcript: ${text}`);
    
    return {
      success: true,
      messageId: messageResult.message_id,
      transcript: text
    };
  } catch (error) {
    console.log("Error:", error.message);
    return { success: false, error: error.message };
  }
})();
```

This code:
1. Gets the current chat context
2. Synthesizes a voice message from text using `Voice.synthesize()`
3. Sends the generated audio to Telegram using `Telegram.sendVoice()`
4. Sends the transcript text as a follow-up message
5. Returns the result with message ID and transcript

You can modify the `text` variable to generate different voice messages, or integrate it with an existing audio URL if you want to transcribe instead.
```

### 10. Create a simple key-value cache with TTL expiration

**Python** (evaluator notes: Correctly implements TTL cache with proper expiration logic. Idiomatic Python with type hints, docstrings, and clear class structure. Minor: could benefit from try/except around time operations, though unlikely to fail in practice.)

```python

```python
import time
from typing import Any, Optional

class TTLCache:
    def __init__(self):
        self.cache: dict[str, tuple[Any, float]] = {}
    
    def set(self, key: str, value: Any, ttl: int) -> None:
        """Store a key-value pair with TTL in seconds"""
        expiration_time = time.time() + ttl
        self.cache[key] = (value, expiration_time)
    
    def get(self, key: str) -> Optional[Any]:
        """Retrieve a value if it exists and hasn't expired"""
        if key not in self.cache:
            return None
        
        value, expiration_time = self.cache[key]
        
        if time.time() > expiration_time:
            del self.cache[key]
            return None
        
        return value
    
    def delete(self, key: str) -> bool:
        """Delete a key if it exists"""
        if key in self.cache:
            del self.cache[key]
            return True
        return False
    
    def clear(self) -> None:
        """Clear all entries"""
        self.cache.clear()
    
    def cleanup_expired(self) -> int:
        """Remove all expired entries, return count of removed items"""
        current_time = time.time()
        expired_keys = [
            key for key, (_, exp_time) in self.cache.items()
            if current_time > exp_time
        ]
        for key in expired_keys:
            del self.cache[key]
        return len(expired_keys)

cache = TTLCache()
cache.set("user:1", {"name": "Alice", "age": 30}, ttl=5)
cache.set("user:2", {"name": "Bob", "age": 25}, ttl=10)

print("Get user:1:", cache.get("user:1"))
print("Get user:2:", cache.get("user:2"))

time.sleep(6)

print("After 6 seconds, user:1:", cache.get("user:1"))
print("After 6 seconds, user:2:", cache.get("user:2"))

cleaned = cache.cleanup_expired()
print(f"Cleaned up {cleaned} expired entries")
```
```

**JavaScript** (evaluator notes: Functionally correct but returns an object with nested 'state' property (only the state is usable, awkward pattern). The async IIFE is unnecessary since no await is used. No actual QuickJS incompatibilities—uses only built-in Map and Date. Error handling minimal. Consider restructuring to return the methods directly.)

```javascript

```javascript
(async () => {
  const cache = new Map();

  const set = (key, value, ttlMs = 60000) => {
    const expiresAt = Date.now() + ttlMs;
    cache.set(key, { value, expiresAt });
  };

  const get = (key) => {
    const entry = cache.get(key);
    if (!entry) return null;
    if (Date.now() > entry.expiresAt) {
      cache.delete(key);
      return null;
    }
    return entry.value;
  };

  const del = (key) => {
    cache.delete(key);
  };

  const clear = () => {
    cache.clear();
  };

  const cleanup = () => {
    const now = Date.now();
    for (const [key, entry] of cache.entries()) {
      if (now > entry.expiresAt) {
        cache.delete(key);
      }
    }
  };

  const size = () => cache.size;

  return {
    state: {
      set,
      get,
      del,
      clear,
      cleanup,
      size
    }
  };
})();
```

This creates a simple TTL cache with:
- **set(key, value, ttlMs)** - Store a value with optional TTL (default 60s)
- **get(key)** - Retrieve a value (returns null if expired or missing)
- **del(key)** - Delete a specific key
- **clear()** - Clear all entries
- **cleanup()** - Remove all expired entries
- **size()** - Get number of cached items

Expired entries are lazily removed on access or via cleanup().
```

## System Prompt Adjustments for JavaScript

The JS system prompt used in this spike includes the following key constraints:

1. **Explicit QuickJS context**: `'You write JavaScript (ES2020) code that runs in QuickJS'`
2. **Negative constraints**: `'Do NOT use: require(), import, fetch(), Node.js APIs, top-level await'`
3. **Available globals listed**: `Telegram.sendMessage()`, `Http.get()`, `Http.post()`, `Voice.*`, `console.log()`
4. **Async wrapper pattern**: `'Wrap your code in (async () => { ... })()'` instead of top-level await

### Observed Improvements vs. Naive Prompt

- Explicit `(async () => { ... })()` pattern prevents top-level await errors
- Listing `Http.get/post` instead of `fetch()` guides correct API usage
- Negative list of banned features reduces accidental `require()`/`import` usage
- Listing exact QuickJS-compatible ES2020 features reduces engine incompatibilities

## Conclusion

Both acceptance criteria are met. JS quality is 166.5% of Python quality, with only 0 QuickJS violation(s). JavaScript (ES2020 / QuickJS) is a viable target language for Oochy agent code generation.
