package emails

import (
	htmltemplate "html/template"
	"os"
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func TestOrganizationWelcomeMail(t *testing.T) {
	tmpl, err := os.ReadFile("../../templates/emails/rebrand.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	ctx := &config.AppContext{
		Env:           &types.EnvConfig{Prod: true, Host: "btcpp.dev"},
		TemplateCache: htmltemplate.Must(htmltemplate.New("emails/rebrand.tmpl").Parse(string(tmpl))),
	}
	membership := &types.OrganizationMembership{OrganizationID: "org-id", PersonID: "person-id", CreatedAt: time.Now(), UpdatedAt: time.Now(), Organization: &types.Org{Name: "Builders & Friends", LogoLight: "/static/img/builders.png"}}
	for _, role := range []string{"member", "manager", "owner"} {
		t.Run(role, func(t *testing.T) {
			membership.Role = role
			mail, err := BuildOrganizationWelcomeMail(ctx, membership, "Amina <script>", "amina@example.com", "https://btcpp.dev/organizations/builders", "https://btcpp.dev/static/img/berlin26/leading.png", "Alex <script>")
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"btcpp-shell", "of the Builders &amp; Friends org at bitcoin++", "Alex &lt;script&gt; added you", `href="https://btcpp.dev/organizations/builders"`, "https://btcpp.dev/static/img/berlin26/leading.png", "Amina &lt;script&gt;", "#F57247", `src="https://btcpp.dev/static/img/builders.png"`, `alt="Builders &amp; Friends logo"`} {
				if !strings.Contains(string(mail.HTMLBody), want) {
					t.Errorf("HTML missing %q", want)
				}
			}
			for _, unwanted := range []string{"<script>", "Accept organization invitation", "Unsubscribe"} {
				if strings.Contains(string(mail.HTMLBody), unwanted) {
					t.Errorf("unexpected HTML %q", unwanted)
				}
			}
			if !strings.Contains(string(mail.TextBody), " "+role) || !strings.Contains(string(mail.TextBody), "https://btcpp.dev/organizations/builders") || strings.Contains(string(mail.TextBody), "<p>") {
				t.Fatal("invalid plain-text alternative")
			}
			// Role/profile updates must not cause launch retries to create another job.
			membership.UpdatedAt = membership.UpdatedAt.Add(time.Hour)
			again, err := BuildOrganizationWelcomeMail(ctx, membership, "Amina", mail.Email, "https://btcpp.dev/organizations/builders", "", "")
			if err == nil && (!strings.Contains(string(again.TextBody), "You've been added") || strings.Contains(string(again.TextBody), "Alex")) {
				t.Fatal("missing inviter must use neutral confirmation")
			}
			if err != nil || again.JobKey != mail.JobKey {
				t.Fatal("retry must retain delivery key")
			}
		})
	}
	membership.Role = "admin"
	if _, err := BuildOrganizationWelcomeMail(ctx, membership, "Amina", "amina@example.com", "https://btcpp.dev/organizations/builders", "", ""); err == nil {
		t.Fatal("unsupported role accepted")
	}
}
