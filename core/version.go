package core

// Version is the keyop version string of the running binary.
//
// The canonical value is linked into github.com/wu/keyop/cmd.Version at build
// time; that package copies it here at startup so services can report the
// running version without importing cmd (which imports every service).
var Version = "dev"
