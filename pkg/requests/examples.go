package requests

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"github.com/stripe/stripe-cli/pkg/config"
)

const (
	validToken        = "tok_visa"
	declinedToken     = "tok_chargeDeclined"
	disputeToken      = "tok_createDisputeInquiry"
	chargeFailedToken = "tok_chargeCustomerFail"
)

func parseResponse(response []byte) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := json.Unmarshal(response, &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// WebhookEndpointList contains the list of webhook endpoints for the account
type WebhookEndpointList struct {
	Data []WebhookEndpoint `json:"data"`
}

// WebhookEndpoint contains the data for each webhook endpoint
type WebhookEndpoint struct {
	Application   string   `json:"application"`
	EnabledEvents []string `json:"enabled_events"`
	URL           string   `json:"url"`
}

// Examples stores possible webhook test events to trigger for the CLI
type Examples struct {
	Profile    config.Profile
	APIBaseURL string
	APIVersion string
	APIKey     string
}

func (ex *Examples) buildRequest(method string, data []string) (*Base, *RequestParameters) {
	params := &RequestParameters{
		data:    data,
		version: ex.APIVersion,
	}

	base := &Base{
		Profile:        &ex.Profile,
		Method:         method,
		SuppressOutput: true,
		APIBaseURL:     ex.APIBaseURL,
	}

	return base, params
}

func (ex *Examples) performStripeRequest(req *Base, endpoint string, params *RequestParameters) (map[string]interface{}, error) {
	resp, err := req.MakeRequest(ex.APIKey, endpoint, params, true)
	if err != nil {
		return nil, err
	}

	return parseResponse(resp)
}

func (ex *Examples) chargeCreated(token string, data []string) (map[string]interface{}, error) {
	paymentSource := fmt.Sprintf("source=%s", token)

	req, params := ex.buildRequest(http.MethodPost, append(data, paymentSource))
	return ex.performStripeRequest(req, "/v1/charges", params)
}

// ChargeCaptured first creates a charge that is not captured, then
// sends another request to specifically capture it to trigger the
// captured event
func (ex *Examples) ChargeCaptured() error {
	charge, err := ex.chargeCreated(validToken, []string{
		"amount=2000",
		"currency=usd",
		"capture=false",
	})

	if err != nil {
		return err
	}

	req, params := ex.buildRequest(http.MethodPost, []string{})
	reqURL := fmt.Sprintf("/v1/charges/%s/capture", charge["id"])
	_, err = ex.performStripeRequest(req, reqURL, params)
	return err
}

// ChargeDisputed creates a charge that becomes disputed
func (ex *Examples) ChargeDisputed() error {
	_, err := ex.chargeCreated(disputeToken, []string{
		"amount=2000",
		"currency=usd",
	})
	return err
}

// ChargeFailed fails to create a charge
func (ex *Examples) ChargeFailed() error {
	_, err := ex.chargeCreated(declinedToken, []string{
		"amount=2000",
		"currency=usd",
	})
	return err
}

// ChargeRefunded creates a charge, then refunds it
func (ex *Examples) ChargeRefunded() error {
	charge, err := ex.chargeCreated(validToken, []string{
		"amount=2000",
		"currency=usd",
	})
	if err != nil {
		return err
	}

	req, params := ex.buildRequest(http.MethodPost, []string{
		fmt.Sprintf("charge=%s", charge["id"]),
	})
	_, err = ex.performStripeRequest(req, "/v1/refunds", params)
	return err
}

// ChargeSucceeded successfully creates a charge
func (ex *Examples) ChargeSucceeded() error {
	_, err := ex.chargeCreated(validToken, []string{
		"amount=2000",
		"currency=usd",
	})
	return err
}

// CheckoutSessionCompleted creates and completes a Checkout session
// https://stripe.com/docs/api/checkout/sessions/create?lang=curl
func (ex *Examples) CheckoutSessionCompleted() error {
	req, params := ex.buildRequest(http.MethodPost, []string{
		"success_url=https://httpbin.org/post",
		"cancel_url=https://httpbin.org/post",
		"payment_method_types[]=card",
		"line_items[][name]=T-shirt",
		"line_items[][description]=Comfortable cotton t-shirt",
		"line_items[][amount]=1500",
		"line_items[][currency]=usd",
		"line_items[][quantity]=2",
	})

	checkoutSession, err := ex.performStripeRequest(req, "/v1/checkout/sessions", params)
	if err != nil {
		return err
	}

	sessID, ok := checkoutSession["id"]
	if !ok {
		return errors.New("Unable to retrieve CheckoutSession ID")
	}

	// Undocumented API GET /v1/payment_pages
	req, params = ex.buildRequest(http.MethodGet, []string{
		fmt.Sprintf("session_id=%s", sessID),
	})

	paymentPage, err := ex.performStripeRequest(req, "/v1/payment_pages", params)
	if err != nil {
		return err
	}

	paymentPageID, ok := paymentPage["id"]
	if !ok {
		return errors.New("Unable to retrieve PaymentPage ID")
	}

	paymentMethod, err := ex.paymentMethodCreatedWithToken(validToken)
	if err != nil {
		return err
	}

	pmID, ok := paymentMethod["id"]
	if !ok {
		return errors.New("Unable to retrieve PaymentMethod ID")
	}

	// Undocumented API POST /v1/payment_pages/<ID>/confirm
	req, params = ex.buildRequest(http.MethodPost, []string{
		fmt.Sprintf("payment_method=%s", pmID),
	})
	_, err = ex.performStripeRequest(req, fmt.Sprintf("/v1/payment_pages/%s/confirm", paymentPageID), params)
	return err
}

func (ex *Examples) customerCreated(data []string) (map[string]interface{}, error) {
	req, params := ex.buildRequest(http.MethodPost, data)
	return ex.performStripeRequest(req, "/v1/customers", params)
}

// CustomerCreated creates a new customer
func (ex *Examples) CustomerCreated() error {
	_, err := ex.customerCreated([]string{})
	return err
}

// CustomerUpdated creates a new customer and adds metadata to
// trigger an update event
func (ex *Examples) CustomerUpdated() error {
	customer, err := ex.customerCreated([]string{})
	if err != nil {
		return err
	}
	req, params := ex.buildRequest(http.MethodPost, []string{
		"metadata[foo]=bar",
	})
	reqURL := fmt.Sprintf("/v1/customers/%s", customer["id"])
	_, err = ex.performStripeRequest(req, reqURL, params)
	return err
}

// CustomerDeleted creates a customer, then deletes it
func (ex *Examples) CustomerDeleted() error {
	customer, err := ex.customerCreated([]string{})
	if err != nil {
		return err
	}
	req, params := ex.buildRequest(http.MethodDelete, []string{})
	reqURL := fmt.Sprintf("/v1/customers/%s", customer["id"])
	_, err = ex.performStripeRequest(req, reqURL, params)
	return err
}

// CustomerSourceCreated creates a customer and a token then attaches
// the card to the customer
func (ex *Examples) CustomerSourceCreated() error {
	customer, err := ex.customerCreated([]string{})
	if err != nil {
		return err
	}

	req, params := ex.buildRequest(http.MethodPost, []string{
		fmt.Sprintf("source=%s", validToken),
	})

	reqURL := fmt.Sprintf("/v1/customers/%s/sources", customer["id"])
	_, err = ex.performStripeRequest(req, reqURL, params)
	return err
}

// CustomerSourceUpdated creates a customer, adds a card,
// adds metadata to the card to trigger an update
func (ex *Examples) CustomerSourceUpdated() error {
	customer, err := ex.customerCreated([]string{})
	if err != nil {
		return err
	}

	req, params := ex.buildRequest(http.MethodPost, []string{
		fmt.Sprintf("source=%s", validToken),
	})

	reqURL := fmt.Sprintf("/v1/customers/%s/sources", customer["id"])
	card, err := ex.performStripeRequest(req, reqURL, params)
	if err != nil {
		return err
	}

	req, params = ex.buildRequest(http.MethodPost, []string{
		"metadata[foo]=bar",
	})
	reqURL = fmt.Sprintf("/v1/customers/%s/sources/%s", customer["id"], card["id"])
	_, err = ex.performStripeRequest(req, reqURL, params)
	return err
}

// CustomerSubscriptionUpdated creates a customer with a card, creates a plan,
// adds the customer to the plan, then updates the new subscription
func (ex *Examples) CustomerSubscriptionUpdated() error {
	customer, err := ex.customerCreated([]string{
		fmt.Sprintf("source=%s", validToken),
	})
	if err != nil {
		return err
	}

	req, params := ex.buildRequest(http.MethodPost, []string{
		"currency=usd",
		"interval=month",
		"amount=2000",
		"product[name]=myproduct",
	})
	plan, err := ex.performStripeRequest(req, "/v1/plans", params)
	if err != nil {
		return err
	}

	req, params = ex.buildRequest(http.MethodPost, []string{
		fmt.Sprintf("items[0][plan]=%s", plan["id"]),
		fmt.Sprintf("customer=%s", customer["id"]),
	})
	subscription, err := ex.performStripeRequest(req, "/v1/subscriptions", params)
	if err != nil {
		return err
	}

	req, params = ex.buildRequest(http.MethodPost, []string{
		"metadata[foo]=bar",
	})
	reqURL := fmt.Sprintf("/v1/subscriptions/%s", subscription["id"])
	_, err = ex.performStripeRequest(req, reqURL, params)
	return err
}

// CustomerSubscriptionDeleted creates a customer with a card, creates a plan,
// adds the customer to the plan, then deletes it
func (ex *Examples) CustomerSubscriptionDeleted() error {
	customer, err := ex.customerCreated([]string{
		fmt.Sprintf("source=%s", validToken),
	})
	if err != nil {
		return err
	}

	req, params := ex.buildRequest(http.MethodPost, []string{
		"currency=usd",
		"interval=month",
		"amount=2000",
		"product[name]=myproduct",
	})
	plan, err := ex.performStripeRequest(req, "/v1/plans", params)
	if err != nil {
		return err
	}

	req, params = ex.buildRequest(http.MethodPost, []string{
		fmt.Sprintf("items[0][plan]=%s", plan["id"]),
		fmt.Sprintf("customer=%s", customer["id"]),
	})
	subscription, err := ex.performStripeRequest(req, "/v1/subscriptions", params)
	if err != nil {
		return err
	}

	req, params = ex.buildRequest(http.MethodDelete, []string{})
	reqURL := fmt.Sprintf("/v1/subscriptions/%s", subscription["id"])
	_, err = ex.performStripeRequest(req, reqURL, params)
	return err
}

func (ex *Examples) createInvoiceItem(data []string) (map[string]interface{}, error) {
	req, params := ex.buildRequest(http.MethodPost, data)
	return ex.performStripeRequest(req, "/v1/invoiceitems", params)
}

func (ex *Examples) invoiceCreated(data []string) (map[string]interface{}, error) {
	req, params := ex.buildRequest(http.MethodPost, data)
	return ex.performStripeRequest(req, "/v1/invoices", params)
}

// InvoiceCreated first creates a customer, adds an invoice item,
// then creates an invoice.
func (ex *Examples) InvoiceCreated() error {
	customer, err := ex.customerCreated([]string{})
	if err != nil {
		return err
	}

	_, err = ex.createInvoiceItem([]string{
		"currency=usd",
		fmt.Sprintf("customer=%s", customer["id"]),
		"amount=2000",
	})
	if err != nil {
		return err
	}

	_, err = ex.invoiceCreated([]string{
		fmt.Sprintf("customer=%s", customer["id"]),
	})
	if err != nil {
		return err
	}

	return err
}

// InvoiceFinalized first creates a customer, adds an invoice item,
// creates an invoice, and then finalizes the invoice.
func (ex *Examples) InvoiceFinalized() error {
	customer, err := ex.customerCreated([]string{})
	if err != nil {
		return err
	}

	_, err = ex.createInvoiceItem([]string{
		"currency=usd",
		fmt.Sprintf("customer=%s", customer["id"]),
		"amount=2000",
	})
	if err != nil {
		return err
	}

	invoice, err := ex.invoiceCreated([]string{
		fmt.Sprintf("customer=%s", customer["id"]),
	})
	if err != nil {
		return err
	}

	req, params := ex.buildRequest(http.MethodPost, []string{})
	reqURL := fmt.Sprintf("/v1/invoices/%s/finalize", invoice["id"])
	_, err = ex.performStripeRequest(req, reqURL, params)
	return err
}

// InvoicePaymentSucceeded first creates a customer, adds an invoice item,
// creates the invoice, and then pays the invoice
func (ex *Examples) InvoicePaymentSucceeded() error {
	customer, err := ex.customerCreated([]string{
		fmt.Sprintf("source=%s", validToken),
	})
	if err != nil {
		return err
	}

	_, err = ex.createInvoiceItem([]string{
		"currency=usd",
		fmt.Sprintf("customer=%s", customer["id"]),
		"amount=2000",
	})
	if err != nil {
		return err
	}

	invoice, err := ex.invoiceCreated([]string{
		fmt.Sprintf("customer=%s", customer["id"]),
	})
	if err != nil {
		return err
	}

	req, params := ex.buildRequest(http.MethodPost, []string{})
	reqURL := fmt.Sprintf("/v1/invoices/%s/pay", invoice["id"])
	_, err = ex.performStripeRequest(req, reqURL, params)
	return err
}

// InvoicePaymentFailed first creates a customer, adds an invoice item,
// creates the invoice, and then fails the payment
func (ex *Examples) InvoicePaymentFailed() error {
	customer, err := ex.customerCreated([]string{
		fmt.Sprintf("source=%s", chargeFailedToken),
	})
	if err != nil {
		return err
	}

	_, err = ex.createInvoiceItem([]string{
		"currency=usd",
		fmt.Sprintf("customer=%s", customer["id"]),
		"amount=2000",
	})
	if err != nil {
		return err
	}

	invoice, err := ex.invoiceCreated([]string{
		fmt.Sprintf("customer=%s", customer["id"]),
	})
	if err != nil {
		return err
	}

	fmt.Println("hello")
	req, params := ex.buildRequest(http.MethodPost, []string{})
	reqURL := fmt.Sprintf("/v1/invoices/%s/pay", invoice["id"])
	_, err = ex.performStripeRequest(req, reqURL, params)
	return err
}

// InvoiceUpdated first creates a customer, adds an invoice item,
// creates the invoice, then adds metadata to the invoice to trigger an update
func (ex *Examples) InvoiceUpdated() error {
	customer, err := ex.customerCreated([]string{})
	if err != nil {
		return err
	}

	_, err = ex.createInvoiceItem([]string{
		"currency=usd",
		fmt.Sprintf("customer=%s", customer["id"]),
		"amount=2000",
	})
	if err != nil {
		return err
	}

	invoice, err := ex.invoiceCreated([]string{
		fmt.Sprintf("customer=%s", customer["id"]),
	})
	if err != nil {
		return err
	}

	req, params := ex.buildRequest(http.MethodPost, []string{
		"metadata[foo]=bar",
	})

	reqURL := fmt.Sprintf("/v1/invoices/%s", invoice["id"])
	_, err = ex.performStripeRequest(req, reqURL, params)
	return err
}

func (ex *Examples) paymentIntentCreated(data []string) (map[string]interface{}, error) {
	req, params := ex.buildRequest(http.MethodPost, data)
	return ex.performStripeRequest(req, "/v1/payment_intents", params)
}

// PaymentIntentCreated creates a payment intent. Requires the data to be assigned
// to the payment intent
func (ex *Examples) PaymentIntentCreated() error {
	_, err := ex.paymentIntentCreated([]string{
		"amount=2000",
		"currency=usd",
		"payment_method_types[]=card",
	})
	return err
}

// PaymentIntentSucceeded creates a successful payment intent
func (ex *Examples) PaymentIntentSucceeded() error {
	paymentMethod, err := ex.paymentMethodCreated("4242424242424242")
	if err != nil {
		return err
	}
	paymentMethodID := fmt.Sprintf("payment_method=%s", paymentMethod["id"])

	_, err = ex.paymentIntentCreated([]string{
		"amount=2000",
		"currency=usd",
		"payment_method_types[]=card",
		"confirm=true",
		paymentMethodID,
	})

	return err
}

// PaymentIntentFailed creates a failed payment intent
func (ex *Examples) PaymentIntentFailed() error {
	paymentMethod, err := ex.paymentMethodCreated("4000000000000002")
	if err != nil {
		return err
	}

	_, err = ex.paymentIntentCreated([]string{
		"amount=2000",
		"currency=usd",
		"payment_method_types[]=card",
		"confirm=true",
		fmt.Sprintf("payment_method=%s", paymentMethod["id"]),
	})

	return err
}

func (ex *Examples) paymentMethodCreated(card string) (map[string]interface{}, error) {
	req, params := ex.buildRequest(http.MethodPost, []string{
		"type=card",
		fmt.Sprintf("card[number]=%s", card),
		"card[exp_month]=12",
		"card[exp_year]=2020",
		"card[cvc]=123",
	})
	return ex.performStripeRequest(req, "/v1/payment_methods", params)
}

func (ex *Examples) paymentMethodCreatedWithToken(token string) (map[string]interface{}, error) {
	req, params := ex.buildRequest(http.MethodPost, []string{
		"type=card",
		fmt.Sprintf("card[token]=%s", token),
		"billing_details[email]=stripe@example.com",
	})
	return ex.performStripeRequest(req, "/v1/payment_methods", params)
}

// PaymentMethodAttached creates a customer and payment method,
// then attaches the customer to the payment method
func (ex *Examples) PaymentMethodAttached() error {
	customer, err := ex.customerCreated([]string{})
	if err != nil {
		return err
	}

	paymentMethod, err := ex.paymentMethodCreated("4242424242424242")
	if err != nil {
		return err
	}

	req, params := ex.buildRequest(http.MethodPost, []string{
		fmt.Sprintf("customer=%s", customer["id"]),
	})
	reqURL := fmt.Sprintf("/v1/payment_methods/%s/attach", paymentMethod["id"])
	_, err = ex.performStripeRequest(req, reqURL, params)
	return err
}

// WebhookEndpointsList returns all the webhook endpoints on a users' account
func (ex *Examples) WebhookEndpointsList() WebhookEndpointList {
	params := &RequestParameters{
		version: ex.APIVersion,
		data:    []string{"limit=30"},
	}

	base := &Base{
		Profile:        &ex.Profile,
		Method:         http.MethodGet,
		SuppressOutput: true,
		APIBaseURL:     ex.APIBaseURL,
	}
	resp, _ := base.MakeRequest(ex.APIKey, "/v1/webhook_endpoints", params, true)
	data := WebhookEndpointList{}
	json.Unmarshal(resp, &data)

	return data
}

// ResendEvent resends a webhook event using it's event-id "evt_<id>"
func (ex *Examples) ResendEvent(id string) error {
	pattern := `^evt_[A-Za-z0-9]{3,255}$`
	match, err := regexp.MatchString(pattern, id)
	if err != nil {
		return err
	}

	if !match {
		return fmt.Errorf("Invalid event-id provided, should be of the form '%s'", pattern)
	}

	req, params := ex.buildRequest(http.MethodPost, []string{})
	reqURL := fmt.Sprintf("/v1/events/%s/retry", id)
	_, err = ex.performStripeRequest(req, reqURL, params)
	return err
}
