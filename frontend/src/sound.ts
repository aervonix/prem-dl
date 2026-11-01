// A short, soft click made with the Web Audio API, so the app ships no sound files.

let ctx: AudioContext | null = null

// Browsers (WebView2 included) only let audio start after the user has interacted with the page,
// so the context is created on the first pointer or key press; finishes come later.
function unlock() {
  if (!ctx) {
    try {
      ctx = new AudioContext()
    } catch {
      return
    }
  }
  if (ctx.state === 'suspended') void ctx.resume()
}
for (const ev of ['pointerdown', 'keydown'] as const) window.addEventListener(ev, unlock, {capture: true, passive: true})

/** Two quick taps, high then lower: reads as a click rather than a beep. */
export function playClick() {
  unlock()
  if (!ctx || ctx.state !== 'running') return
  const t = ctx.currentTime
  for (const [freq, at, peak] of [
    [2200, 0, 0.16],
    [1400, 0.045, 0.12],
  ]) {
    const osc = ctx.createOscillator()
    const gain = ctx.createGain()
    osc.type = 'triangle'
    osc.frequency.value = freq
    gain.gain.setValueAtTime(0, t + at)
    gain.gain.linearRampToValueAtTime(peak, t + at + 0.002)
    gain.gain.exponentialRampToValueAtTime(0.0001, t + at + 0.05)
    osc.connect(gain).connect(ctx.destination)
    osc.start(t + at)
    osc.stop(t + at + 0.06)
  }
}

// Typing sounds: a soft foam-damped key, made the same way. A foam keyboard has no click and no
// ping, only a muffled "thock" when the key bottoms out and a fainter one when it comes back up.
// Each press is a short burst of filtered noise (the keycap hitting foam) over a falling sine
// (the case's thump). Each key gets its own slight pitch, as on a real board, and each press a
// little randomness, so fast typing doesn't sound like one sample on repeat.

type KeyKind = 'key' | 'space' | 'enter' | 'back'

interface KeySound {
  band: number // Hz, the noise's centre
  thump: number // Hz, where the sine starts
  decay: number // s
  level: number
}

const KINDS: Record<KeyKind, KeySound> = {
  key: {band: 760, thump: 260, decay: 0.045, level: 1},
  back: {band: 660, thump: 240, decay: 0.05, level: 1},
  enter: {band: 560, thump: 200, decay: 0.065, level: 1.1},
  space: {band: 460, thump: 170, decay: 0.08, level: 1.15},
}

const VOLUME = 0.28
const RELEASE = 0.3 // the up-stroke's level against the press

let noise: AudioBuffer | null = null
function noiseBuffer(c: AudioContext): AudioBuffer {
  if (!noise || noise.sampleRate !== c.sampleRate) {
    noise = c.createBuffer(1, Math.round(c.sampleRate * 0.3), c.sampleRate)
    const d = noise.getChannelData(0)
    for (let i = 0; i < d.length; i++) d[i] = Math.random() * 2 - 1
  }
  return noise
}

/** A stable offset in [-1, 1] for a key, so the same key always sounds a touch the same. */
function keyTone(code: string): number {
  let h = 0
  for (let i = 0; i < code.length; i++) h = (h * 31 + code.charCodeAt(i)) | 0
  return ((h >>> 0) % 1000) / 500 - 1
}

const jitter = (amount: number) => 1 + (Math.random() * 2 - 1) * amount

/** One key sound into ctx's output; up is the quieter, brighter release. */
export function playKey(kind: KeyKind = 'key', code = '', up = false) {
  unlock()
  if (!ctx || ctx.state !== 'running') return
  const c = ctx
  const k = KINDS[kind]
  const t = c.currentTime + 0.002
  const tone = 1 + keyTone(code) * 0.06
  const level = VOLUME * k.level * (up ? RELEASE : 1) * jitter(0.15)
  const decay = k.decay * (up ? 0.6 : 1) * jitter(0.1)

  const out = c.createGain()
  out.gain.value = level
  // Foam takes the top off everything.
  const muffle = c.createBiquadFilter()
  muffle.type = 'lowpass'
  muffle.frequency.value = up ? 2600 : 1900
  muffle.Q.value = 0.5
  muffle.connect(out).connect(c.destination)

  // The keycap: band-passed noise, a fast attack and a quick exponential fall.
  const src = c.createBufferSource()
  src.buffer = noiseBuffer(c)
  const band = c.createBiquadFilter()
  band.type = 'bandpass'
  band.frequency.value = k.band * tone * (up ? 1.5 : 1) * jitter(0.08)
  band.Q.value = 1.1
  const hit = c.createGain()
  hit.gain.setValueAtTime(0, t)
  hit.gain.linearRampToValueAtTime(1, t + 0.0015)
  hit.gain.exponentialRampToValueAtTime(0.001, t + decay)
  src.connect(band).connect(hit).connect(muffle)
  const offset = Math.random() * 0.2
  src.start(t, offset, decay + 0.01)

  // The thump: a sine that drops in pitch as it dies, felt more than heard.
  if (!up) {
    const osc = c.createOscillator()
    osc.type = 'sine'
    const f = k.thump * tone * jitter(0.05)
    osc.frequency.setValueAtTime(f, t)
    osc.frequency.exponentialRampToValueAtTime(f * 0.6, t + decay)
    const body = c.createGain()
    body.gain.setValueAtTime(0, t)
    body.gain.linearRampToValueAtTime(0.12, t + 0.002)
    body.gain.exponentialRampToValueAtTime(0.001, t + decay * 1.2)
    osc.connect(body).connect(muffle)
    osc.start(t)
    osc.stop(t + decay * 1.2 + 0.01)
  }
}

const MODIFIERS = new Set(['Shift', 'Control', 'Alt', 'AltGraph', 'Meta', 'OS', 'CapsLock', 'Fn', 'NumLock', 'ScrollLock'])
const TEXT_INPUTS = new Set(['', 'text', 'search', 'url', 'email', 'password', 'tel', 'number'])

/** Whether t is a box the user types into (not a checkbox, button or read-only field). */
function typable(t: EventTarget | null): boolean {
  if (!(t instanceof HTMLElement)) return false
  if (t instanceof HTMLTextAreaElement) return !t.readOnly && !t.disabled
  if (t instanceof HTMLInputElement) return TEXT_INPUTS.has(t.type) && !t.readOnly && !t.disabled
  return t.isContentEditable
}

function kindOf(key: string): KeyKind {
  if (key === ' ') return 'space'
  if (key === 'Enter') return 'enter'
  if (key === 'Backspace' || key === 'Delete') return 'back'
  return 'key'
}

let typing = false
const down = new Set<string>() // keys whose press made a sound, so their release does too

function onKeyDown(e: KeyboardEvent) {
  // Held keys repeat in software; a real key goes down once.
  if (e.repeat || e.isComposing || MODIFIERS.has(e.key) || !typable(e.target)) return
  down.add(e.code)
  playKey(kindOf(e.key), e.code)
}

function onKeyUp(e: KeyboardEvent) {
  if (!down.delete(e.code)) return
  playKey(kindOf(e.key), e.code, true)
}

function onBlur() {
  down.clear()
}

/** Turns typing sounds in text boxes on or off. */
export function setTypingSound(on: boolean) {
  if (on === typing) return
  typing = on
  if (on) {
    window.addEventListener('keydown', onKeyDown, true)
    window.addEventListener('keyup', onKeyUp, true)
    window.addEventListener('blur', onBlur) // the window's own, not its boxes'
  } else {
    window.removeEventListener('keydown', onKeyDown, true)
    window.removeEventListener('keyup', onKeyUp, true)
    window.removeEventListener('blur', onBlur)
    down.clear()
  }
}
