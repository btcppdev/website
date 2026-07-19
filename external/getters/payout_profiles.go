package getters

import (
	"fmt"
	"strings"

	"btcpp-web/internal/config"
)

func SetPersonTaxForm(ctx *config.AppContext, personID, formType, objectKey, originalName string) error {
	formType = strings.ToLower(strings.TrimSpace(formType))
	if formType != "w9" && formType != "w8ben" {
		return fmt.Errorf("tax form type must be W-9 or W-8BEN")
	}
	if strings.TrimSpace(objectKey) == "" {
		return fmt.Errorf("tax form object key is required")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE people
		SET tax_form_type = $2,
			tax_form_object_key = $3,
			tax_form_original_name = $4,
			tax_form_uploaded_at = now()
		WHERE id = $1::uuid
	`, personID, formType, objectKey, originalName)
	if err != nil {
		return fmt.Errorf("save person tax form metadata: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("person not found")
	}
	return nil
}
