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
