module github.com/improbabilitea/soap-go

go 1.27.1

// The minimum stays 1.27.1 for the modules that use this one; builds of this
// module itself take 1.27.2, which fixes GO-2026-6608/6610/6611/6613/6617 in
// net/http and net/textproto.
toolchain go1.27.2
