package expedition

import _ "embed"

// These defaults are reviewed and published by a global administrator. They are
// not automatically activated and do not replace previously accepted snapshots.
//
//go:embed policy/terms.txt
var BookingTerms string

//go:embed policy/privacy.txt
var BookingPrivacy string
