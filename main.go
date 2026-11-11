// Command erp-costing runs the costing component alone (serve, migrate); the shells compile in costing.Definition().
package main

import (
	"github.com/brickKit/be-sdk-go/v3/launcher"
	"github.com/brickKit/erp-costing/v3/costing"
)

func main() { launcher.Main(costing.Definition()) }
