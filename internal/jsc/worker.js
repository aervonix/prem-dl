// Prem-dl's YouTube challenge solver: yt-dlp's own (yt-dlp-ejs, the lib and core scripts of the
// bundled yt-dlp) kept running, so the 3 MB player script is parsed once per player version
// instead of on every yt-dlp run. Messages are JSON, one per line, on stdin and stdout:
//
//   {"op":"init","lib":"...","core":"..."}                     -> {"type":"ready"}
//   {"op":"solve","player_url":"...","player":"...","requests":[{"type":"n","challenges":[...]}]}
//                                -> {"type":"result","responses":[...]}, as yt-dlp's solver answers
//                                   {"type":"need_player"} when the player isn't known yet and wasn't sent
//                                   {"type":"error","error":"..."}
'use strict';

const MAX_PLAYERS = 4;
const players = new Map(); // player URL -> {preprocessed, solvers}; the newest last

function init(msg) {
  // Indirect eval: the scripts declare their globals (var lib, var jsc) as yt-dlp's stdin does.
  (0, eval)(msg.lib);
  Object.assign(globalThis, globalThis.lib);
  (0, eval)(msg.core);
  if (typeof globalThis.jsc !== 'function') return {type: 'error', error: 'the solver scripts did not define jsc'};
  return {type: 'ready'};
}

// The n and sig functions of a preprocessed player, as the core script's getFromPrepared builds
// them; null when that no longer works, and each request then goes through jsc().
function solversOf(preprocessed) {
  try {
    const result = {n: null, sig: null};
    Function('_result', preprocessed)(result);
    return result;
  } catch {
    return null;
  }
}

function remember(url, preprocessed) {
  players.delete(url);
  players.set(url, {preprocessed, solvers: solversOf(preprocessed)});
  while (players.size > MAX_PLAYERS) players.delete(players.keys().next().value);
  return players.get(url);
}

// The core script's answer for each request, from the kept functions.
function answer(solvers, requests) {
  return requests.map((r) => {
    if (r.type !== 'n' && r.type !== 'sig') return {type: 'error', error: `Unknown request type: ${r.type}`};
    const solver = solvers[r.type];
    if (!solver) return {type: 'error', error: `Failed to extract ${r.type} function`};
    try {
      return {type: 'result', data: Object.fromEntries(r.challenges.map((c) => [c, solver(c)]))};
    } catch (error) {
      return {type: 'error', error: error instanceof Error ? `${error.message}\n${error.stack}` : `${error}`};
    }
  });
}

function solve(msg) {
  let p = players.get(msg.player_url);
  if (!p) {
    if (!msg.player) return {type: 'need_player'};
    const out = globalThis.jsc({type: 'player', player: msg.player, requests: msg.requests, output_preprocessed: true});
    if (out.type !== 'result') return out;
    remember(msg.player_url, out.preprocessed_player);
    return {type: 'result', responses: out.responses};
  }
  if (p.solvers) return {type: 'result', responses: answer(p.solvers, msg.requests)};
  return globalThis.jsc({type: 'preprocessed', preprocessed_player: p.preprocessed, requests: msg.requests});
}

function handle(line) {
  let msg;
  try {
    msg = JSON.parse(line);
    if (msg.op === 'init') return init(msg);
    if (msg.op === 'solve') return solve(msg);
    return {type: 'error', error: `unknown op ${msg.op}`};
  } catch (error) {
    return {type: 'error', error: error instanceof Error ? `${error.message}\n${error.stack}` : `${error}`};
  }
}

let pending = '';
process.stdin.setEncoding('utf8');
process.stdin.on('data', (chunk) => {
  pending += chunk;
  let i;
  while ((i = pending.indexOf('\n')) >= 0) {
    const line = pending.slice(0, i);
    pending = pending.slice(i + 1);
    if (line.trim()) process.stdout.write(JSON.stringify(handle(line)) + '\n');
  }
});
process.stdin.on('end', () => process.exit(0));
