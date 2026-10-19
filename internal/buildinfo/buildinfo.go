package buildinfo

// Version is injected by release builds. The default keeps source/dev runs from
// treating themselves as installable release binaries.
var Version = "dev"

// DevelopmentVersion is the public capability target of this source tree.
const DevelopmentVersion = "0.6.1"

const Repository = "alfredxw/denova"
