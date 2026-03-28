# Reminder

A simple keyword-triggered reminder manager that stores reminders persistently using Storage. Designed for GUI chat use — all interaction happens via text commands and return values (no Telegram required).

## What it does

Responds to natural-language commands to add, list, and complete reminders. All data is persisted in Storage so reminders survive across sessions.

## Trigger

This skill is triggered when your message contains the keyword **remind**, **reminder**, or **알림**.

## Commands

| Command | Example | Description |
|---|---|---|
| `remind <text>` | `remind buy groceries` | Add a new reminder |
| `알림 <text>` | `알림 회의 준비` | Add a reminder (Korean) |
| `list` | `list` | Show all active and completed reminders |
| `목록` | `목록` | Show reminders (Korean) |
| `done <n>` | `done 2` | Mark reminder #2 as done |
| `완료 <n>` | `완료 2` | Mark reminder #n as done (Korean) |
| `clear` | `clear` | Remove all completed reminders |

## Configuration

No configuration required. Reminders are stored in the built-in `Storage` primitive under the key `reminders`.

## Example Session

```
User:  remind review pull request #42
Bot:   ✏️ Reminder #1 saved: "review pull request #42"
       Say "list" to see all reminders.

User:  remind submit expense report by Friday
Bot:   ✏️ Reminder #2 saved: "submit expense report by Friday"

User:  list
Bot:   📋 Your Reminders

       Active:
         [1] review pull request #42  (2025-03-28)
         [2] submit expense report by Friday  (2025-03-28)

User:  done 1
Bot:   ✅ Marked reminder #1 as done: "review pull request #42"
```

## Notes

- Reminder IDs are stable integers that never reuse within a session.
- Use `clear` to remove completed items and keep the list tidy.
- Storage is shared within your KittyPaw installation, so reminders persist across skill invocations.
