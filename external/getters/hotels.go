package getters

import (
	"fmt"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

// HotelInput is the shape every Hotel write goes through. Empty
// strings mean "leave the field unset" on create and "clear it" on
// update — Notion-side validation is loose, mirroring how
// ProposalInput is handled. Order is always written (zero is a
// real, valid display rank).
type HotelInput struct {
	Name    string
	URL     string
	Img     string // bare Spaces path, e.g. "vienna/hotels/abc.jpg"
	Type    string
	Desc    string
	Order   int
	ConfRef string // Conference page ID for the `conf` relation
}

func listHotels(ctx *config.AppContext) ([]*types.Hotel, error) {
	if UsePostgresBackend(ctx) {
		return listHotelsPostgres(ctx)
	}
	return ListHotelsNotion(ctx.Notion)
}

func ListHotels(n *types.Notion) ([]*types.Hotel, error) {
	return ListHotelsNotion(n)
}

func ListHotelsForConf(ctx *config.AppContext, confRef string) ([]*types.Hotel, error) {
	if UsePostgresBackend(ctx) {
		return listHotelsForConfPostgres(ctx, confRef)
	}
	allHotels, err := listHotels(ctx)
	if err != nil {
		return nil, err
	}
	hotels := make([]*types.Hotel, 0)
	for _, hotel := range allHotels {
		if hotel != nil && hotel.ConfRef == confRef {
			hotels = append(hotels, hotel)
		}
	}
	return hotels, nil
}

// CreateHotel inserts a new row into the Hotels DB and returns the
// new page ID. ConfRef is required (no orphan hotels); everything
// else is optional and gets written when non-empty.
func CreateHotel(ctx *config.AppContext, in HotelInput) (string, error) {
	if UsePostgresBackend(ctx) {
		return createHotelPostgres(ctx, in)
	}
	if in.ConfRef == "" {
		return "", fmt.Errorf("CreateHotel: ConfRef is required")
	}
	return createHotelNotion(ctx.Notion, in)
}

// UpdateHotel patches an existing Hotel row. Empty fields are left
// untouched on update so a partial form post doesn't accidentally
// blank a field the admin didn't intend to clear.
func UpdateHotel(ctx *config.AppContext, hotelID string, in HotelInput) error {
	if UsePostgresBackend(ctx) {
		return updateHotelPostgres(ctx, hotelID, in)
	}
	return updateHotelNotion(ctx, hotelID, in)
}

// ArchiveHotel soft-deletes a Hotel row (Notion archive — recoverable
// from the trash for 30 days). Goes through raw HTTP PATCH because
// the go-notion library doesn't expose the `archived` flag on its
// UpdatePageProperties wrapper, mirroring DeleteConfTalk.
func ArchiveHotel(ctx *config.AppContext, hotelID string) error {
	if UsePostgresBackend(ctx) {
		return archiveHotelPostgres(ctx, hotelID)
	}
	return archiveHotelNotion(ctx, hotelID)
}
