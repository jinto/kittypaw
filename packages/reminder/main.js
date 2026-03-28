// reminder/main.js
// Keyword-triggered reminder manager.
// Commands (all case-insensitive):
//   "remind <text>"    — add a reminder
//   "list" / "목록"    — list all active reminders
//   "done <n>" / "완료 <n>" — mark reminder #n as done
//   "clear"            — remove all completed reminders
// Responds via return value (no Telegram — designed for GUI chat).

const ctx = JSON.parse(__context__);
// The triggering message is available on the context
const message = ((ctx.message && ctx.message.text) || ctx.input || "").trim();

const STORAGE_KEY = "reminders";

// --- Load reminders from Storage ---
// Each reminder: { id, text, done, createdAt }
async function loadReminders() {
  try {
    const raw = await Storage.get(STORAGE_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    const value = parsed.value !== undefined ? parsed.value : parsed;
    return typeof value === "string" ? JSON.parse(value) : (value || []);
  } catch (e) {
    return [];
  }
}

// --- Save reminders to Storage ---
async function saveReminders(reminders) {
  await Storage.set(STORAGE_KEY, JSON.stringify(reminders));
}

// --- Generate a simple auto-increment ID ---
function nextId(reminders) {
  if (reminders.length === 0) return 1;
  return Math.max(...reminders.map(r => r.id)) + 1;
}

// --- Format reminder list for display ---
function formatList(reminders) {
  const active = reminders.filter(r => !r.done);
  const done = reminders.filter(r => r.done);
  if (reminders.length === 0) return "No reminders yet. Say: remind <something>";

  const lines = ["📋 Your Reminders\n"];
  if (active.length > 0) {
    lines.push("Active:");
    active.forEach(r => lines.push(`  [${r.id}] ${r.text}  (${r.createdAt})`));
  }
  if (done.length > 0) {
    lines.push("\nCompleted:");
    done.forEach(r => lines.push(`  [${r.id}] ✓ ${r.text}`));
  }
  return lines.join("\n");
}

// --- Parse command ---
const lower = message.toLowerCase();
let reminders = await loadReminders();

// LIST
if (lower === "list" || lower === "목록" || lower === "reminders") {
  return formatList(reminders);
}

// CLEAR completed
if (lower === "clear") {
  const before = reminders.length;
  reminders = reminders.filter(r => !r.done);
  await saveReminders(reminders);
  const removed = before - reminders.length;
  return `Cleared ${removed} completed reminder(s). ${reminders.length} active remaining.`;
}

// DONE <n> or 완료 <n>
const doneMatch = message.match(/^(?:done|완료)\s+(\d+)$/i);
if (doneMatch) {
  const id = parseInt(doneMatch[1], 10);
  const idx = reminders.findIndex(r => r.id === id);
  if (idx === -1) return `Reminder #${id} not found.`;
  reminders[idx].done = true;
  await saveReminders(reminders);
  return `✅ Marked reminder #${id} as done: "${reminders[idx].text}"`;
}

// REMIND <text> or 알림 <text>
const remindMatch = message.match(/^(?:remind(?:er)?|알림)\s+(.+)$/i);
if (remindMatch) {
  const text = remindMatch[1].trim();
  if (!text) return "Please provide a reminder text. Example: remind buy groceries";
  const newReminder = {
    id: nextId(reminders),
    text,
    done: false,
    createdAt: new Date().toISOString().slice(0, 10),
  };
  reminders.push(newReminder);
  await saveReminders(reminders);
  return `✏️ Reminder #${newReminder.id} saved: "${text}"\n\nSay "list" to see all reminders.`;
}

// Fallback: show help
return [
  "Reminder Commands:",
  "  remind <text>    — add a new reminder",
  "  list             — show all reminders",
  "  done <n>         — mark reminder #n as done",
  "  clear            — remove completed reminders",
  "",
  `You said: "${message}"`,
].join("\n");
