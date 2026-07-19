package types

import "time"

const (
	ShopCheckoutSessionTTL      = 30 * time.Minute
	ShopInventoryReservationTTL = ShopCheckoutSessionTTL + 5*time.Minute

	MerchProductStatusDraft     = "draft"
	MerchProductStatusPublished = "published"
	MerchProductStatusArchived  = "archived"

	MerchInventoryPolicyDeny           = "deny"
	MerchInventoryPolicyAllowBackorder = "allow_backorder"
	MerchInventoryPolicyUnlimited      = "unlimited"

	ShopOrderStatusPending           = "pending"
	ShopOrderStatusPaid              = "paid"
	ShopOrderStatusCancelled         = "cancelled"
	ShopOrderStatusRefunded          = "refunded"
	ShopOrderStatusPartiallyRefunded = "partially_refunded"

	ShopOrderSourceOnline = "online"
	ShopOrderSourcePOS    = "pos"
	ShopOrderSourceAdmin  = "admin"

	ShopCheckoutKindMerch  = "merch"
	ShopCheckoutKindTicket = "ticket"
	ShopCheckoutKindMixed  = "mixed"

	ShopFulfillmentShip        = "ship"
	ShopFulfillmentEventPickup = "event_pickup"
	ShopFulfillmentPOSTakeaway = "pos_takeaway"

	ShopItemStatusPending           = "pending"
	ShopItemStatusReady             = "ready"
	ShopItemStatusFulfilled         = "fulfilled"
	ShopItemStatusCancelled         = "cancelled"
	ShopItemStatusRefunded          = "refunded"
	ShopItemStatusPartiallyRefunded = "partially_refunded"

	ShopActorSystem    = "system"
	ShopActorAdmin     = "admin"
	ShopActorBuyer     = "buyer"
	ShopActorVolunteer = "volunteer"
	ShopActorPOS       = "pos"

	TaxProviderStripe        = "stripe"
	ShippingProviderEasyship = "easyship"

	StripeTaxCodeNontaxable   = "txcd_00000000"
	StripeTaxCodeTangibleGood = "txcd_99999999"
	StripeTaxCodeShipping     = "txcd_92010001"
)

type MerchProduct struct {
	ID               string
	Tag              string
	Slug             string
	Name             string
	Subtitle         string
	Description      string
	Status           string
	ProductType      string
	BasePriceCents   uint
	Currency         string
	Symbol           string
	PostSymbol       string
	StripeTaxCode    string
	EasyshipCategory string
	HSCode           string
	CountryOfOrigin  string
	RequiresShipping bool
	AllowEventPickup bool
	AvailableFrom    *time.Time
	AvailableUntil   *time.Time
	Images           []*MerchProductImage
	Options          []*MerchProductOption
	Variants         []*MerchVariant
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type MerchProductImage struct {
	ID           string
	ProductID    string
	ObjectKey    string
	AltText      string
	DisplayOrder int
	IsPrimary    bool
	CreatedAt    time.Time
}

type MerchProductOption struct {
	ID           string
	ProductID    string
	Name         string
	DisplayOrder int
	Required     bool
	Values       []*MerchProductOptionValue
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type MerchProductOptionValue struct {
	ID           string
	OptionID     string
	Value        string
	DisplayOrder int
	CreatedAt    time.Time
}

type MerchVariant struct {
	ID              string
	ProductID       string
	SKU             string
	Label           string
	PriceDeltaCents int
	Stock           int
	WeightGrams     int
	LengthMM        int
	WidthMM         int
	HeightMM        int
	InventoryPolicy string
	Status          string
	OptionValues    []*MerchProductOptionValue
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type ShopOrder struct {
	ID                          string
	PublicID                    string
	BuyerEmail                  string
	BuyerName                   string
	Status                      string
	Source                      string
	CheckoutKind                string
	PaymentProvider             string
	PaymentProviderID           string
	AdminNotes                  string
	Currency                    string
	SubtotalCents               uint
	DiscountAmountCents         uint
	ShippingAmountCents         uint
	SalesTaxAmountCents         uint
	ImportDutyAmountCents       uint
	ImportTaxAmountCents        uint
	TotalCents                  uint
	PaidAt                      *time.Time
	CancelledAt                 *time.Time
	CheckoutExpiresAt           *time.Time
	ShippingAddress             *ShopAddress
	Items                       []*ShopOrderItem
	Shipments                   []*Shipment
	UnfulfilledShippingQuantity uint
	UnfulfilledShippingSummary  string
	EventPickupQuantity         uint
	EventPickupSummary          string
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
}

type ShopAddress struct {
	Name       string
	Line1      string
	Line2      string
	City       string
	Region     string
	PostalCode string
	Country    string
	Phone      string
}

type ShopRefundContact struct {
	Signal   string
	Telegram string
}

type ShopOperationalStats struct {
	PendingOrders         uint
	ExpiredPendingOrders  uint
	ExpiredReservations   uint
	PaidMissingProviderID uint
	LowStock              []*ShopLowStockItem
}

type ShopLowStockItem struct {
	ProductName string
	VariantID   string
	SKU         string
	Label       string
	Stock       int
}

type ShopOrderItem struct {
	ID                   string
	OrderID              string
	ProductID            string
	VariantID            string
	Quantity             uint
	FulfilledQuantity    uint
	RefundedQuantity     uint
	UnitPriceCents       uint
	DiscountAmountCents  uint
	TaxAmountCents       uint
	LineTotalCents       uint
	ProductTagSnapshot   string
	ProductNameSnapshot  string
	VariantLabelSnapshot string
	SKUSnapshot          string
	ImageObjectKey       string
	FulfillmentMethod    string
	SaleConferenceID     string
	SaleConferenceTag    string
	PickupConferenceID   string
	Status               string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type ShopItemPickup struct {
	ID           string
	OrderItemID  string
	ConferenceID string
	Quantity     uint
	PickedUpBy   string
	PickedUpAt   time.Time
	Notes        string
}

type MerchInventoryEvent struct {
	ID            string
	VariantID     string
	EventType     string
	QuantityDelta int
	OrderItemID   string
	ConferenceID  string
	ActorEmail    string
	Notes         string
	OccurredAt    time.Time
	CreatedAt     time.Time
}

type MerchInventoryReservation struct {
	ID                string
	VariantID         string
	CheckoutSessionID string
	Quantity          uint
	Status            string
	ExpiresAt         time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type ShippingRateQuote struct {
	ID                    string
	OrderID               string
	Provider              string
	ProviderQuoteID       string
	CourierServiceID      string
	DestinationCountry    string
	DestinationRegion     string
	DestinationPostalCode string
	CourierName           string
	ServiceName           string
	AmountCents           uint
	Currency              string
	EstimatedMinDays      *int
	EstimatedMaxDays      *int
	RawResponse           string
	ExpiresAt             *time.Time
	CreatedAt             time.Time
}

type Shipment struct {
	ID                   string
	OrderID              string
	Provider             string
	ProviderShipmentID   string
	ProviderLabelID      string
	CourierServiceID     string
	CourierName          string
	ServiceName          string
	TrackingNumber       string
	TrackingURL          string
	LabelURL             string
	Status               string
	LabelState           string
	DeliveryState        string
	RawResponse          string
	ShippedAt            *time.Time
	DeliveredAt          *time.Time
	LastWebhookAt        *time.Time
	LastSyncedAt         *time.Time
	ShippingNotifiedAt   *time.Time
	CreateIdempotencyKey string
	LabelIdempotencyKey  string
	LastError            string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type ShipmentParcelItem struct {
	OrderItemID   string
	Quantity      uint
	SKU           string
	Description   string
	ValueCents    uint
	WeightGrams   int
	LengthMM      int
	WidthMM       int
	HeightMM      int
	HSCode        string
	Category      string
	OriginCountry string
}

type TaxQuote struct {
	ID                    string
	OrderID               string
	SalesTaxProvider      string
	SalesTaxAmountCents   uint
	ImportProvider        string
	ImportDutyAmountCents uint
	ImportTaxAmountCents  uint
	Incoterm              string
	DestinationCountry    string
	DestinationRegion     string
	DestinationPostalCode string
	RawTaxResponse        string
	RawImportResponse     string
	ExpiresAt             *time.Time
	CreatedAt             time.Time
}

type TaxTransaction struct {
	ID                    string
	OrderID               string
	Provider              string
	ProviderTransactionID string
	SalesTaxAmountCents   uint
	Status                string
	RawResponse           string
	RecordedAt            time.Time
	VoidedAt              *time.Time
}

type Refund struct {
	ID               string
	OrderID          string
	Provider         string
	ProviderRefundID string
	AmountCents      uint
	Currency         string
	Reason           string
	Status           string
	RequestedBy      string
	RawResponse      string
	Items            []*RefundItem
	CreatedAt        time.Time
	CompletedAt      *time.Time
}

type RefundItem struct {
	RefundID    string
	OrderItemID string
	Quantity    uint
	AmountCents uint
	Restock     bool
}

type ShopEvent struct {
	ID           string
	EventType    string
	ActorType    string
	ActorEmail   string
	EntityType   string
	EntityID     string
	OrderID      string
	OrderItemID  string
	ProductID    string
	VariantID    string
	ConferenceID string
	Metadata     string
	CreatedAt    time.Time
}
