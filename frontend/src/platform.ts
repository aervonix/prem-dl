/**
 * What the UI calls the parts of the system it mentions. Windows keeps its own names; elsewhere
 * (Linux) the words are the desktop's general ones.
 */
export interface SystemWords {
  /** The theme option that follows the system. */
  matchTheme: string
  /** The setting for the system's notifications. */
  notifications: string
  /** The setting that starts the app at sign-in. */
  startAtSignIn: string
  /** Where the tray icon lives: "in the notification area". */
  inTray: string
}

export function systemWords(platform: string): SystemWords {
  if (platform === 'windows') {
    return {
      matchTheme: 'Match Windows',
      notifications: 'Windows notifications',
      startAtSignIn: 'Start with Windows',
      inTray: 'in the notification area',
    }
  }
  return {
    matchTheme: 'Match system',
    notifications: 'Desktop notifications',
    startAtSignIn: 'Start when you sign in',
    inTray: 'in the tray',
  }
}
