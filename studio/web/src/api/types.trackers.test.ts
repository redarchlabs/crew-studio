import { describe, expect, it } from 'vitest'
import { isTrackerKind, trackerKindLabel } from './types.trackers'

describe('tracker kinds', () => {
  it('knows Jira, Linear and KM2 issues as trackers', () => {
    expect(['jira', 'linear', 'km2_tracker'].every(isTrackerKind)).toBe(true)
    expect(isTrackerKind('km2')).toBe(false)
    expect(isTrackerKind('toString')).toBe(false)
  })

  it('names them', () => {
    expect(trackerKindLabel('jira')).toBe('Jira')
    expect(trackerKindLabel('linear')).toBe('Linear')
    expect(trackerKindLabel('km2_tracker')).toBe('KM2')
    expect(trackerKindLabel('http')).toBe('http')
  })
})
