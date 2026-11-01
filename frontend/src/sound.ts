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

// Typing sounds: a foam-damped mechanical keyboard, made the same way. Tuned against a recording
// of a foamed Keychron Q3: a key is a few milliseconds of tight, plasticky click with resonances
// near 600 Hz, 1.6 kHz and 2.6 kHz (the cap, the switch, the plate), often with a smaller second
// hit as the cap settles, then about as loud a click again on the way up. The foam keeps it short
// and takes off the top. A long, low ring is what made the first version sound like wood.
//
// Each hit is a burst of noise ringing a few band-pass filters (one per resonance). Each key is
// tuned a touch differently, as on a real board, and each press varies a little, so fast typing
// doesn't sound like one sample on repeat.

type KeyKind = 'key' | 'space' | 'enter' | 'back'

/** A resonance: frequency (Hz), sharpness (Q) and level. */
type Mode = [number, number, number]

interface KeySound {
  modes: Mode[]
  burst: number // s, how long the noise that rings them lasts
  settle: number // s after the hit when the cap's smaller second hit comes; 0 = none
  level: number
}

const KINDS: Record<KeyKind, KeySound> = {
  key: {
    modes: [[450, 4, 0.4], [760, 5, 0.8], [1250, 5, 0.9], [1650, 7, 1], [2400, 12, 1.3], [2700, 10, 0.25]],
    burst: 0.005,
    settle: 0.022,
    level: 1,
  },
  back: {
    modes: [[500, 5, 0.5], [700, 7, 1.6], [820, 7, 1.1], [1700, 7, 0.4], [2400, 9, 0.15]],
    burst: 0.006,
    settle: 0.025,
    level: 1,
  },
  enter: {
    modes: [[450, 5, 1], [720, 10, 1.6], [850, 7, 0.9], [1150, 7, 0.3]],
    burst: 0.02,
    settle: 0.03,
    level: 1.05,
  },
  // The long bar rattles a little on its stabilisers, and sits lower.
  space: {
    modes: [[400, 6, 2], [520, 6, 1.2], [800, 6, 1], [1150, 7, 1], [1650, 8, 1.3], [1850, 8, 0.9]],
    burst: 0.007,
    settle: 0.018,
    level: 1.1,
  },
}

const VOLUME = 0.4
const RELEASE = 0.65 // the up-stroke's level against the press
const SETTLE = 0.4 // the second hit's level against the first

let noise: AudioBuffer | null = null
function noiseBuffer(c: BaseAudioContext): AudioBuffer {
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

/** One hit at t: a burst of noise ringing the key's resonances, into dest. */
function hit(c: BaseAudioContext, dest: AudioNode, t: number, k: KeySound, tune: number, level: number) {
  const src = c.createBufferSource()
  src.buffer = noiseBuffer(c)
  const env = c.createGain()
  env.gain.setValueAtTime(0, t)
  env.gain.linearRampToValueAtTime(level, t + 0.0005)
  env.gain.exponentialRampToValueAtTime(level * 0.001, t + k.burst)
  src.connect(env)
  for (const [f, q, g] of k.modes) {
    const band = c.createBiquadFilter()
    band.type = 'bandpass'
    band.frequency.value = f * tune * jitter(0.02)
    band.Q.value = q
    const gain = c.createGain()
    gain.gain.value = g
    env.connect(band).connect(gain).connect(dest)
  }
  src.start(t, Math.random() * 0.2, k.burst + 0.02)
}

/** One key sound into ctx's output; up is the release, a little higher and quieter. */
export function playKey(kind: KeyKind = 'key', code = '', up = false) {
  unlock()
  if (!ctx || ctx.state !== 'running') return
  const c = ctx
  const k = KINDS[kind]
  const t = c.currentTime + 0.002
  const tune = (1 + keyTone(code) * 0.05) * (up ? 1.06 : 1)
  const level = VOLUME * k.level * (up ? RELEASE : 1) * jitter(0.15)

  // Foam takes the top off.
  const muffle = c.createBiquadFilter()
  muffle.type = 'lowpass'
  muffle.frequency.value = 3300
  muffle.Q.value = 0.7
  muffle.connect(c.destination)

  hit(c, muffle, t, k, tune, level)
  if (k.settle && !up) hit(c, muffle, t + k.settle * jitter(0.25), k, tune * 1.03, level * SETTLE * jitter(0.3))
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
