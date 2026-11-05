package main

import (
	"errors"
	"strings"
)

// Controller/TUN readiness alone cannot prove that traffic still reaches TUN.
// Verify the complete active plan, including borrowed scoped direct routes;
// borrowed routes are never added to the ownership journal or removed on exit.
func verifyTransparentRoutePlan(routes []transparentRoute, command func(string, ...string) ([]byte, error)) (diagnosticDetails, error) {
	tables := map[string][]byte{}
	for i, r := range routes {
		family := strings.TrimPrefix(routeFamily(r.Prefix), "-")
		table, ok := tables[family]
		if !ok {
			var err error
			table, err = command("/usr/sbin/netstat", "-rn", "-f", family)
			if err != nil {
				return diagnosticDetails{}, err
			}
			tables[family] = table
		}
		present := ownedTransparentRouteInTable(table, r)
		if r.Scoped && r.Gateway != "" {
			present = scopedDefaultInTable(table, originalNetwork{Interface: r.Interface}, r.Gateway)
		}
		if r.Pending || !present {
			return diagnosticDetails{RouteIndex: &i, Interface: r.Interface, Scoped: &r.Scoped}, errors.New("分流或直连路由已改变，已停止本次连接，请重新连接。")
		}
	}
	return diagnosticDetails{}, nil
}
