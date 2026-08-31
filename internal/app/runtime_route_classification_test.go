package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

type routeAccessClass string

const (
	routePublic   routeAccessClass = "public"
	routeUser     routeAccessClass = "user"
	routeBusiness routeAccessClass = "business"
)

func TestEveryGinRouteRegistrationHasAnExplicitAccessClass(t *testing.T) {
	t.Parallel()

	classes := map[string]routeAccessClass{
		"router": routePublic,
		"api":    routePublic,
		"auth":   routePublic,
		"public": routePublic,
		"store":  routePublic,

		"mcpGroup":   routeUser,
		"googleAuth": routeUser,
		"admin":      routeUser,

		"protected":            routeBusiness,
		"dashboard":            routeBusiness,
		"businesses":           routeBusiness,
		"customers":            routeBusiness,
		"vendors":              routeBusiness,
		"products":             routeBusiness,
		"projects":             routeBusiness,
		"reports":              routeBusiness,
		"warehouses":           routeBusiness,
		"inventory":            routeBusiness,
		"assemblies":           routeBusiness,
		"barcodes":             routeBusiness,
		"group":                routeBusiness,
		"invoices":             routeBusiness,
		"payments":             routeBusiness,
		"documents":            routeBusiness,
		"priceLists":           routeBusiness,
		"partyGroups":          routeBusiness,
		"activityLogs":         routeBusiness,
		"signatures":           routeBusiness,
		"bulkJobs":             routeBusiness,
		"imports":              routeBusiness,
		"invoiceSubscriptions": routeBusiness,
		"journals":             routeBusiness,
		"renderProfiles":       routeBusiness,
		"utils":                routeBusiness,
		"tax":                  routeBusiness,
		"pos":                  routeBusiness,
		"shipments":            routeBusiness,
		"ledger":               routeBusiness,
		"teams":                routeBusiness,
		"webhooks":             routeBusiness,
		"notifications":        routeBusiness,
		"subscriptions":        routeBusiness,
		"branches":             routeBusiness,
		"roles":                routeBusiness,
		"storefronts":          routeBusiness,
		"drive":                routeBusiness,
		"whatsapp":             routeBusiness,
		"email":                routeBusiness,
		"agents":               routeBusiness,
		"shopping":             routeBusiness,
		"credentials":          routeBusiness,
		"config":               routeBusiness,
		"mentee":               routeBusiness,
		"marketplace":          routeBusiness,
		"discovery":            routeBusiness,
		"llm":                  routeBusiness,
		"voice":                routeBusiness,
		"voiceSessions":        routeBusiness,
		"workflows":            routeBusiness,
		"intent":               routeBusiness,
		"ws":                   routeBusiness,
		"bargaining":           routeBusiness,
		"a2aBargaining":        routeBusiness,
	}

	file, err := parser.ParseFile(token.NewFileSet(), "runtime.go", nil, 0)
	if err != nil {
		t.Fatalf("parse runtime routes: %v", err)
	}
	methods := map[string]bool{
		"Any": true, "DELETE": true, "GET": true, "HEAD": true,
		"OPTIONS": true, "PATCH": true, "POST": true, "PUT": true,
	}
	counts := map[routeAccessClass]int{}

	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !methods[selector.Sel.Name] {
			return true
		}
		receiver, ok := selector.X.(*ast.Ident)
		if !ok {
			t.Errorf("route %s has a non-identifier receiver", selector.Sel.Name)
			return true
		}
		class, ok := classes[receiver.Name]
		if !ok {
			t.Errorf("route group %q is not classified", receiver.Name)
			return true
		}
		counts[class]++
		return true
	})

	for _, class := range []routeAccessClass{routePublic, routeUser, routeBusiness} {
		if counts[class] == 0 {
			t.Errorf("route class %q has no registered routes", class)
		}
	}
}

func TestRouteAccessRootsRetainApplicationDualCognitoAndBusinessMiddleware(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("runtime.go")
	if err != nil {
		t.Fatalf("read runtime routes: %v", err)
	}
	routes := string(source)
	for _, required := range []string{
		`mcpGroup.Use(middleware.Auth(cfg.Cognito, log))`,
		`googleAuth.Use(middleware.AuthWithTokenUse(cfg.Cognito, log, middleware.TokenUseID))`,
		`protected.Use(middleware.Auth(cfg.Cognito, log))`,
		`protected.Use(middleware.BusinessAuth(svcs.BusinessAuth))`,
		`admin.Use(middleware.Auth(cfg.Cognito, log))`,
	} {
		if !strings.Contains(routes, required) {
			t.Errorf("route access root is missing %q", required)
		}
	}

	authSource, err := os.ReadFile("../middleware/auth.go")
	if err != nil {
		t.Fatalf("read Cognito middleware: %v", err)
	}
	for _, required := range []string{
		"cognitoPoolsFromConfig(cfg)",
		"cfg.Phone.UserPoolID",
		"cfg.Phone.ClientID",
	} {
		if !strings.Contains(string(authSource), required) {
			t.Errorf("Cognito middleware is missing dual-pool behavior %q", required)
		}
	}
}
