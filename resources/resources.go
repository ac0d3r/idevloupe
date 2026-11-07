// Package resources holds the files built into the program.
package resources

import _ "embed"

// DevicesTable is a copy of 3uTools' device table (devices_table.txt):
// colors by model and color code, release dates, screens, chips and
// battery makers. A newer copy in the user's 3uTools cache is preferred.
//
//go:embed devices_table.txt
var DevicesTable []byte
