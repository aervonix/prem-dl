/** File name templates offered by name: in Settings, and per preset or download. */
export const TEMPLATES = [
  {value: '%(title)s.%(ext)s', label: 'Title  (default)'},
  {value: '%(title)s [%(id)s].%(ext)s', label: 'Title [id]  (two videos with one title both save)'},
  {value: '%(uploader)s - %(title)s.%(ext)s', label: 'Uploader - Title'},
  {value: '%(upload_date>%Y-%m-%d)s %(title)s.%(ext)s', label: 'Date Title'},
  {value: '%(channel)s/%(title)s.%(ext)s', label: 'Channel folder / Title'},
]

/** The problem with a typed template, or ''. Mirrors settings.checkTemplate. */
export function templateProblem(t: string): string {
  const v = t.trim()
  if (!v) return ''
  if (!v.includes('%(ext)s')) return 'The template must end with .%(ext)s so files keep their extension.'
  if (v.startsWith('-')) return "The template can't start with a dash."
  return ''
}
