package handlers

import "btcpp-web/internal/types"

type adminRoleOption struct {
	Value, Label, Description string
	Restricted                bool
}

func adminRoleOptions(confs []*types.Conf) []adminRoleOption {
	out := []adminRoleOption{
		{"global-admin", "Global administrator", "Manage the whole website and assign roles. Accounts administration is granted separately.", false},
		{"merch-admin", "Merch administrator", "Manage products, stock, orders, fulfillment and refunds. Receive new merch-sale emails.", false},
		{"accts-admin", "Accounts administrator", "Access accounts.btcpp.dev. Only nifty@btcpp.dev can grant this role.", true},
		{"global-volcoord", "Volunteer coordinator · all events", "Manage volunteer operations across all conferences.", false},
		{"global-staff", "Staff · all events", "Staff access across all conferences.", false},
		{"global-hackathon", "Hackathon manager · all events", "Manage hackathons across conferences. Judging access is assigned separately.", false},
	}
	for _, conf := range confs {
		if conf.Tag == "" {
			continue
		}
		for _, r := range []struct{ tag, label, description string }{{"admin", "Administrator", "Administer this conference."}, {"volcoord", "Volunteer coordinator", "Manage this conference's volunteers."}, {"staff", "Staff", "Staff access for this conference."}, {"hackathon", "Hackathon manager", "Manage this conference's hackathon; judging access is separate."}} {
			out = append(out, adminRoleOption{conf.Tag + "-" + r.tag, r.label + " · " + conf.Tag, r.description, false})
		}
	}
	return out
}
