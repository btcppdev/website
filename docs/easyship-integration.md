# Easyship integration plan

## Current boundary

Checkout requests available Easyship services, requires the buyer to choose
one, revalidates that choice during order creation, and persists the selected
rate on the order. A global admin can create an idempotent Easyship shipment
from a paid order and then purchase its label in a separate, confirmation-gated
step. Label URLs and tracking are persisted and signed, replay-safe callbacks
continue updating label and delivery state asynchronously. Cancellation and
provider reconciliation are not implemented yet.

The Rates client uses Easyship Public API `2024-09`. Configure a sandbox token
and base URL locally:

```text
EASYSHIP_API_KEY=<sandbox token beginning with sand>
EASYSHIP_ENDPOINT=https://public-api-sandbox.easyship.com
EASYSHIP_API_VERSION=2024-09
```

Configure the fulfillment origin at `/admin/easyship`. The address is stored in
Postgres so global administrators can update it without changing deployment
secrets or restarting the application. API tokens, endpoint, API version, and
webhook secret remain environment configuration.

Production uses `https://public-api.easyship.com`. The API connection needs at
least `public.rate:read`, `public.shipment:write`, and `public.label:write` for
the intended flow. Keep sandbox and production tokens in separate deployment
secrets.

## Target lifecycle

1. **Quote at checkout**
   - Build parcel items from server-side product and variant data.
   - Request current rates using the completed destination address.
   - Preserve `courier_service_id`, cost, currency, delivery window, and the
     raw rate response with the order.
   - Let the buyer choose among the available services and show each price and
     estimated delivery window.
   - Reject missing weight, dimensions, category/HS code, origin country, or a
     rate currency that cannot be charged safely.

2. **Create a shipment after payment** — implemented
   - Add an admin `Create Easyship shipment` action for a paid order with
     unfulfilled shipped items.
   - Rebuild the request from the immutable order/address snapshots. Never use
     browser-submitted prices or product measurements.
   - Set the shop order ID in Easyship metadata/order data and use the saved
     `courier_service_id` when it remains valid.
   - Persist the Easyship shipment ID and response before label purchase.
   - Make creation idempotent so retries cannot create duplicate shipments.

3. **Buy and retrieve a label** — implemented
   - Keep this as a separate admin action with a confirmation because it books
     a courier and spends Easyship balance.
   - Create the label for the persisted shipment and service, then store its
     label state, label URL, tracking number, tracking URL, courier, and raw
     response.
   - Support asynchronous label completion in addition to the synchronous
     endpoint. A pending label is not a failed or shipped parcel.
   - Show label download/print, tracking, retry, and error details on the admin
     order page.

4. **Receive signed webhooks** — implemented
   - `POST /callbacks/easyship` verifies `X-EASYSHIP-SIGNATURE` as HS256 using
     `EASYSHIP_WEBHOOK_SECRET=<webh_...>`.
   - A SHA-256 payload digest uniquely identifies replayed deliveries.
   - A five-second worker processes label-created, label-failed,
     shipment-cancelled, tracking-status-changed, and
     tracking-checkpoint-created events.
   - Unknown simulator shipment IDs and unsupported event types are retained as
     ignored events without triggering retries or changing an order.

5. **Fulfillment and notifications**
   - Associate a shipment with explicit order-item quantities. The current
     manual action fulfills every shippable item, which cannot model split or
     partial shipments.
   - Mark only those quantities fulfilled when the parcel is actually handed
     to the courier (or when an admin explicitly confirms handoff), not merely
     when a label exists.
   - Send one shipping email when tracking first becomes available. Do not
     resend it on webhook replay.
   - Update delivered, returned, failed, and cancelled states without changing
     unrelated pickup or ticket lines.

6. **Cancellation and reconciliation**
   - Delete a shipment before a label is requested; cancel/void it after label
     purchase while it is still pre-transit.
   - Add a reconciliation job/admin action that retrieves Easyship state for
     shipments stuck in pending or label generation.
   - Surface orders with duplicate shipments, failed labels, missing tracking,
     or webhook state older than an operational threshold.

## Data changes still required

- Add shipment-to-order-item quantity rows for partial/split fulfillment.
- Expand the current immutable `shipment_items` snapshot to support multiple
  active parcels and partial/split fulfillment. The present workflow creates
  one active Easyship shipment for all outstanding shipped quantities.

## Build order

1. Current Rates API request, response parsing, and sandbox quote smoke test. — complete
2. Shipment/item snapshots and idempotent admin shipment creation. — complete
3. Explicit label purchase plus label/tracking display. — complete
4. Signed, replay-safe webhooks. — complete
5. Partial fulfillment, shipping email, cancellation, and reconciliation.
6. Optional customer rate selection, multi-parcel packing, pickups/manifests,
   and returns.

Sandbox verification must stop short of production label purchase. Exercise a
domestic and international quote, create a sandbox shipment, generate a
sandbox label, replay webhook tests, update sandbox tracking state, and test
cancellation. Then repeat one low-cost end-to-end shipment in production with
an explicit admin confirmation.
