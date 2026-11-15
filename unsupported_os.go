//go:build !linux

package main

var _ = "touchzones does not support running on os other than linux" + error_this_project_only_supports_linux
