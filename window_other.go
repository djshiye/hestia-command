//go:build !windows

package main

import "fmt"

func openWindow(url string)        { fmt.Println("Open:", url) }
func showError(title, text string) { fmt.Println(title+":", text) }
