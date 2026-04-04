// kbo-scores/main.js
// Fetches today's KBO baseball scores from koreabaseball.com,
// highlights the user's team if configured, and sends results to Telegram.
// Uses Storage to persist the user's team preference.

const ctx = JSON.parse(__context__);
const config = ctx.config || {};

const telegramToken = config.telegram_token;
const chatId = config.chat_id;
const myTeam = config.my_team || "";

// --- Build today's date string (KST) ---
const now = new Date();
const kstOffset = 9 * 60 * 60 * 1000;
const kst = new Date(now.getTime() + kstOffset);
const year = kst.getUTCFullYear();
const month = String(kst.getUTCMonth() + 1).padStart(2, "0");
const day = String(kst.getUTCDate()).padStart(2, "0");
const dateParam = `${year}${month}${day}`;
const dateDisplay = `${year}.${month}.${day}`;

// --- Persist team preference ---
if (myTeam) {
  try {
    await Storage.set("kbo-scores:my_team", myTeam);
  } catch (_) {}
}

let savedTeam = myTeam;
if (!savedTeam) {
  try {
    savedTeam = (await Storage.get("kbo-scores:my_team")) || "";
  } catch (_) {
    savedTeam = "";
  }
}

// --- Fetch scoreboard page ---
// The KBO schedule page accepts date parameters and returns HTML with game data.
const url =
  `https://www.koreabaseball.com/Schedule/GameCenter/Main.aspx` +
  `?gameDate=${dateParam}`;

let html;
try {
  html = await Http.get(url);
} catch (e) {
  return `Error fetching KBO scores: ${e}`;
}

// --- Parse games from HTML ---
// The GameCenter page embeds game cards with team names and scores.
// Each game block contains patterns like:
//   class="team-info"... team names and scores in structured HTML.
//
// Strategy: extract game blocks, then pull team names and scores from each.

const games = [];

// Pattern 1: Game summary rows — the page uses spans with team names and scores.
// Look for pairs of teams with their runs in score-board sections.
// The HTML contains game data in <li> blocks or table rows.

// Try extracting from the game list items that contain matchup info.
// KBO GameCenter uses a pattern: away team, score, vs, score, home team.
const gameBlockRegex =
  /class="game-cont"[\s\S]*?<\/div>\s*<\/div>\s*<\/li>/gi;
const blocks = html.match(gameBlockRegex) || [];

for (const block of blocks) {
  // Extract team names — they appear in elements like <span class="team">팀명</span>
  const teamMatches = block.match(
    /class="(?:team|teamName|name)"[^>]*>([^<]+)</gi
  );
  // Extract scores — they appear in elements like <span class="score">숫자</span>
  const scoreMatches = block.match(
    /class="(?:score|run)"[^>]*>(\d+)</gi
  );

  if (teamMatches && teamMatches.length >= 2 && scoreMatches) {
    const extractText = (m) => m.replace(/<[^>]+>/g, "").replace(/.*>/, "").trim();
    const extractNum = (m) => {
      const d = m.match(/(\d+)/);
      return d ? d[1] : "?";
    };
    games.push({
      away: extractText(teamMatches[0]),
      home: extractText(teamMatches[1]),
      awayScore: extractNum(scoreMatches[0] || ""),
      homeScore: extractNum(scoreMatches[1] || ""),
    });
  }
}

// Fallback: try a broader pattern if the above didn't match.
// The KBO site sometimes renders scores in <td> or <em> tags.
if (games.length === 0) {
  // Try matching team-score pairs from any table-like structure.
  // Look for patterns: team name followed by a number (the score).
  const KBO_TEAMS = [
    "LG", "KT", "SSG", "NC", "두산", "KIA", "롯데",
    "삼성", "한화", "키움",
  ];
  const teamPattern = KBO_TEAMS.join("|");
  // Match lines like: "LG ... 5 ... 삼성 ... 3" or structured pairs.
  const pairRegex = new RegExp(
    `(${teamPattern})\\s*(?:<[^>]*>\\s*)*?(\\d+)` +
    `[\\s\\S]*?` +
    `(${teamPattern})\\s*(?:<[^>]*>\\s*)*?(\\d+)`,
    "g"
  );

  let pairMatch;
  const seen = new Set();
  while ((pairMatch = pairRegex.exec(html)) !== null) {
    const key = `${pairMatch[1]}-${pairMatch[3]}`;
    const keyRev = `${pairMatch[3]}-${pairMatch[1]}`;
    if (!seen.has(key) && !seen.has(keyRev)) {
      seen.add(key);
      games.push({
        away: pairMatch[1],
        home: pairMatch[3],
        awayScore: pairMatch[2],
        homeScore: pairMatch[4],
      });
    }
  }
}

// --- Handle no-game day ---
if (games.length === 0) {
  const noGameMsg = `baseball  *KBO ${dateDisplay}*\n\n오늘은 경기가 없습니다.`;
  await Telegram.sendMessage(telegramToken, chatId, noGameMsg, {
    parse_mode: "Markdown",
  });
  return `No KBO games found for ${dateDisplay}.`;
}

// --- Format results ---
const teamNorm = savedTeam.trim().toUpperCase();

const gameLines = games.map((g) => {
  const awayUpper = g.away.trim().toUpperCase();
  const homeUpper = g.home.trim().toUpperCase();
  const isMyGame = teamNorm && (awayUpper === teamNorm || homeUpper === teamNorm);

  const awayWin = Number(g.awayScore) > Number(g.homeScore);
  const homeWin = Number(g.homeScore) > Number(g.awayScore);
  const isDraw = g.awayScore === g.homeScore;

  const awayMark = awayWin ? " W" : "";
  const homeMark = homeWin ? " W" : "";
  const drawMark = isDraw ? " (DRAW)" : "";

  const line =
    `${g.away} ${g.awayScore}${awayMark}  vs  ${g.home} ${g.homeScore}${homeMark}${drawMark}`;

  return isMyGame ? `>> ${line} <<` : `   ${line}`;
});

// Build the message
const header = `baseball  *KBO ${dateDisplay} 경기 결과*`;
const teamNote = savedTeam ? `\n_My Team: ${savedTeam}_` : "";
const body = "```\n" + gameLines.join("\n") + "\n```";
const footer = "_Powered by KittyPaw_";

const message = [header, teamNote, "", body, "", footer].join("\n");

await Telegram.sendMessage(telegramToken, chatId, message, {
  parse_mode: "Markdown",
});

return `KBO scores sent: ${games.length} games on ${dateDisplay}.`;
