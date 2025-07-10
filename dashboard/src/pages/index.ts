// Export all pages for easier imports
export { default as Dashboard } from './Dashboard'
export { default as RunDetails } from './RunDetails'
export { default as Compare } from './Compare'
export { default as Baselines } from './Baselines'
export { default as NotFound } from './NotFound'

// Re-export page-related types if any are defined in the future
export type * from './Dashboard'
export type * from './RunDetails'
export type * from './Compare'
export type * from './Baselines'
export type * from './NotFound'