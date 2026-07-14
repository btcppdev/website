package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"fmt"
	"strings"
)

// HotelInput is the shape every Hotel write goes through. Empty strings mean
// "leave the field unset" on create and "clear it" on update. Order is always
// written (zero is a real, valid display rank).
type HotelInput struct {
	Name    string
	URL     string
	Img     string // bare Spaces path, e.g. "vienna/hotels/abc.jpg"
	Type    string
	Desc    string
	Order   int
	ConfRef string // Conference page ID for the `conf` relation
}

// CreateHotel inserts a new row into the Hotels DB and returns the
// new page ID. ConfRef is required (no orphan hotels); everything
// else is optional and gets written when non-empty.

// UpdateHotel patches an existing Hotel row. Empty fields are left
// untouched on update so a partial form post doesn't accidentally
// blank a field the admin didn't intend to clear.

// ArchiveHotel soft-deletes a Hotel row.

func CreateHotel(ctx *config.AppContext, in HotelInput) (string, error) {
	if ctx == nil || ctx.DB == nil {
		return "", fmt.Errorf("database is not configured")
	}
	if strings.TrimSpace(in.ConfRef) == "" {
		return "", fmt.Errorf("CreateHotel: ConfRef is required")
	}
	var hotelID string
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO hotels (
			conference_id, name, url, img_path, type, description, display_order
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7
		)
		RETURNING id::text
	`, in.ConfRef, in.Name, in.URL, in.Img, in.Type, in.Desc, in.Order).Scan(&hotelID)
	if err != nil {
		return "", fmt.Errorf("insert hotel %q: %w", in.Name, err)
	}
	return hotelID, nil
}

func listHotels(ctx *config.AppContext) ([]*types.Hotel, error) {
	return queryHotelsPostgres(ctx, "hotels", "", nil)
}

func ListHotelsForConf(ctx *config.AppContext, confRef string) ([]*types.Hotel, error) {
	confRef = strings.TrimSpace(confRef)
	if confRef == "" {
		return nil, nil
	}
	return queryHotelsPostgres(ctx, "hotels for conf", "AND hotels.conference_id::text = $1", []any{confRef})
}

func queryHotelsPostgres(ctx *config.AppContext, label string, whereSQL string, args []any) ([]*types.Hotel, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT hotels.id::text, hotels.conference_id::text, hotels.name, hotels.url,
			hotels.img_path, hotels.type, hotels.description, hotels.display_order
		FROM hotels
		JOIN conferences ON conferences.id = hotels.conference_id
		WHERE hotels.archived_at IS NULL
		`+whereSQL+`
		ORDER BY conferences.start_date NULLS LAST, hotels.display_order, hotels.name
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", label, err)
	}
	defer rows.Close()

	var hotels []*types.Hotel
	for rows.Next() {
		var hotel types.Hotel
		err := rows.Scan(
			&hotel.ID,
			&hotel.ConfRef,
			&hotel.Name,
			&hotel.URL,
			&hotel.Img,
			&hotel.Type,
			&hotel.Desc,
			&hotel.Order,
		)
		if err != nil {
			return nil, fmt.Errorf("scan %s: %w", label, err)
		}
		hotels = append(hotels, &hotel)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s: %w", label, err)
	}
	return hotels, nil
}

func UpdateHotel(ctx *config.AppContext, hotelID string, in HotelInput) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	if strings.TrimSpace(hotelID) == "" {
		return fmt.Errorf("UpdateHotel: hotelID is required")
	}
	setParts := []string{"display_order = $2", "url = $3", "img_path = $4", "type = $5", "description = $6"}
	args := []interface{}{hotelID, in.Order, in.URL, in.Img, in.Type, in.Desc}
	if in.Name != "" {
		args = append(args, in.Name)
		setParts = append(setParts, fmt.Sprintf("name = $%d", len(args)))
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE hotels
		SET `+strings.Join(setParts, ", ")+`
		WHERE id = $1
	`, args...)
	if err != nil {
		return fmt.Errorf("update hotel %s: %w", hotelID, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("hotel %s not found", hotelID)
	}
	return nil
}

func ArchiveHotel(ctx *config.AppContext, hotelID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE hotels
		SET archived_at = now()
		WHERE id = $1
	`, hotelID)
	if err != nil {
		return fmt.Errorf("archive hotel %s: %w", hotelID, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("hotel %s not found", hotelID)
	}
	return nil
}
