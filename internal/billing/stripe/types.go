package stripe

// The wire shapes, holding only the fields the platform reads. A field that
// is not here is a field nobody can start depending on by accident — which
// is the point: the plan comes from the price id and the registry, never
// from a product's name.

type listPage[T any] struct {
	Data    []T  `json:"data"`
	HasMore bool `json:"has_more"`
}

type apiErrorBody struct {
	Error struct {
		Type    string `json:"type"`
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type price struct {
	ID         string `json:"id"`
	Currency   string `json:"currency"`
	UnitAmount *int64 `json:"unit_amount"`
	Recurring  *struct {
		Interval  string `json:"interval"`
		UsageType string `json:"usage_type"`
	} `json:"recurring"`
	TaxBehavior     string `json:"tax_behavior"`
	CurrencyOptions map[string]struct {
		UnitAmount *int64 `json:"unit_amount"`
	} `json:"currency_options"`
}

type subscriptionItem struct {
	ID               string `json:"id"`
	Quantity         int    `json:"quantity"`
	CurrentPeriodEnd int64  `json:"current_period_end"`
	Price            price  `json:"price"`
}

type subscription struct {
	ID                string                     `json:"id"`
	Customer          string                     `json:"customer"`
	Status            string                     `json:"status"`
	CancelAtPeriodEnd bool                       `json:"cancel_at_period_end"`
	CurrentPeriodEnd  int64                      `json:"current_period_end"`
	Currency          string                     `json:"currency"`
	Metadata          map[string]string          `json:"metadata"`
	Items             listPage[subscriptionItem] `json:"items"`
}

// dispute carries the charge expanded through to its invoice, which is the
// only route from a chargeback to the subscription it concerns.
type dispute struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Charge *struct {
		Invoice *struct {
			Subscription string `json:"subscription"`
		} `json:"invoice"`
	} `json:"charge"`
}
