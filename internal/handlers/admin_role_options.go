package handlers

import "btcpp-web/internal/types"

type adminRoleOption struct {
	Value, Label, Description, Group string
	Restricted                       bool
}

func adminRoleOptions(confs []*types.Conf) []adminRoleOption {
	out := []adminRoleOption{
		{"global-admin", "Global administrator", "Manage the whole website and assign roles. Accounts and Alerts administration are granted separately.", "services", false},
		{"insider-reporter", "Insider reporter", "View and edit all Insider posts. Create stories and schedule your own posts.", "services", false},
		{"insider-contributor", "Insider contributor", "View, edit and review only assigned Insider posts. Cannot schedule or publish posts.", "services", false},
		{"insider-editor", "Insider editor", "Edit, schedule and publish all Insider content, manage contributors and deliveries, and grant complimentary access.", "services", false},
		{"alerts-admin", "Alerts administrator", "Manage the release catalog, delivery retries and service health at alerts.btcpp.dev.", "services", false},
		{"merch-admin", "Merch administrator", "Manage products, stock, orders, fulfillment and refunds. Receive new merch-sale emails.", "services", false},
		{"accts-admin", "Accounts administrator", "Access accounts.btcpp.dev. Only nifty@btcpp.dev can grant this role.", "services", true},
		{"global-volcoord", "Volunteer coordinator · all events", "Manage volunteer operations across all conferences.", "global", false},
		{"global-staff", "Staff · all events", "Staff access across all conferences.", "global", false},
		{"global-hackathon", "Hackathon manager · all events", "Manage hackathons across conferences. Judging access is assigned separately.", "global", false},
	}
	for _, conf := range confs {
		if conf.Tag == "" {
			continue
		}
		for _, r := range []struct{ tag, label, description string }{{"admin", "Administrator", "Administer this conference."}, {"volcoord", "Volunteer coordinator", "Manage this conference's volunteers."}, {"staff", "Staff", "Staff access for this conference."}, {"hackathon", "Hackathon manager", "Manage this conference's hackathon; judging access is separate."}} {
			out = append(out, adminRoleOption{conf.Tag + "-" + r.tag, r.label + " · " + conf.Tag, r.description, "events", false})
		}
	}
	return out
}
