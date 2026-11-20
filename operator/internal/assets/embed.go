package assets

import "embed"

//go:embed kube-dashboard/* upstream/* rbac/* metrics-server/* auth/*
var FS embed.FS
