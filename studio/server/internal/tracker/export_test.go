package tracker

import "time"

// SetKM2Limits lowers the KM2 client's read limits for a test; the returned func restores them.
func SetKM2Limits(peopleCache, inLimit, maxComments int) func() {
	p, i, c := km2PeopleCacheMax, km2InLimit, maxKM2Comments
	km2PeopleCacheMax, km2InLimit, maxKM2Comments = peopleCache, inLimit, maxComments
	return func() { km2PeopleCacheMax, km2InLimit, maxKM2Comments = p, i, c }
}

// WithKM2Cache gives options the shared KM2 people cache a Service gives them, on the given clock.
func WithKM2Cache(o Options, now func() time.Time) Options {
	o.km2 = newKM2Caches(now)
	return o
}
