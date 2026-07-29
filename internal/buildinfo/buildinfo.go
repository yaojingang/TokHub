package buildinfo

// Version is the TokHub release version. Release builds may override it with
// -ldflags while deploy/scripts/version-check.sh keeps this default aligned
// with the repository VERSION file.
var Version = "2.0.0-rc.1"
