package getters

import (
	"context"
	"reflect"
	"testing"

	"btcpp-web/internal/mtypes"
)

func TestSubscriptionsFromNames(t *testing.T) {
	subscriptions := subscriptionsFromNames([]string{"alpha", "beta"})
	if len(subscriptions) != 2 || subscriptions[0].Name != "alpha" || subscriptions[1].Name != "beta" {
		t.Fatalf("subscriptions = %+v", subscriptions)
	}
}

func TestDatabaseSmokeSubscriberQueriesAggregateSubscriptions(t *testing.T) {
	ctx := databaseSmokeContext(t)
	email := "subscriber-" + databaseSmokeSuffix() + "@example.test"
	var subscriberID string
	if err := ctx.DB.QueryRow(context.Background(), `
		INSERT INTO subscribers (email) VALUES ($1) RETURNING id::text
	`, email).Scan(&subscriberID); err != nil {
		t.Fatalf("insert subscriber: %s", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM subscribers WHERE id = $1::uuid`, subscriberID)
	})
	if _, err := ctx.DB.Exec(context.Background(), `
		INSERT INTO subscriber_subscriptions (subscriber_id, name)
		VALUES ($1::uuid, 'beta'), ($1::uuid, 'alpha')
	`, subscriberID); err != nil {
		t.Fatalf("insert subscriptions: %s", err)
	}

	subscriber, err := FindSubscriber(ctx, email)
	if err != nil {
		t.Fatalf("find subscriber: %s", err)
	}
	if subscriber == nil || !reflect.DeepEqual(subscriberSubscriptionNames(subscriber), []string{"alpha", "beta"}) {
		t.Fatalf("subscriber = %+v", subscriber)
	}

	subscribers, err := ListSubscribersFor(ctx, []string{"alpha"})
	if err != nil {
		t.Fatalf("list subscribers: %s", err)
	}
	var found bool
	for _, candidate := range subscribers {
		if candidate.Email == email {
			found = true
			if !reflect.DeepEqual(subscriberSubscriptionNames(candidate), []string{"alpha", "beta"}) {
				t.Fatalf("listed subscriptions = %+v", candidate.Subs)
			}
		}
	}
	if !found {
		t.Fatalf("subscriber %s not returned", email)
	}

	subscribers, err = ListSubscribersFor(ctx, []string{"alpha", "!beta"})
	if err != nil {
		t.Fatalf("list excluded subscribers: %s", err)
	}
	for _, candidate := range subscribers {
		if candidate.Email == email {
			t.Fatalf("excluded subscriber %s was returned", email)
		}
	}
}

func subscriberSubscriptionNames(subscriber *mtypes.Subscriber) []string {
	names := make([]string, 0, len(subscriber.Subs))
	for _, subscription := range subscriber.Subs {
		if subscription != nil {
			names = append(names, subscription.Name)
		}
	}
	return names
}
