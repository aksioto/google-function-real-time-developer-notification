package google_cloud_functions

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/awa/go-iap/playstore"
	"github.com/volatiletech/null"
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

const jsonKey = `service_account__json_key` // service account key

type WebhookReqMessage struct {
	Data string `json:"data"`
}
type WebhookReq struct {
	Message *WebhookReqMessage `json:"message"`
}

type SubscriptionPurchaseNotification struct {
	Version          string `json:"version"`
	NotificationType int    `json:"notificationType"`
	PurchaseToken    string `json:"purchaseToken"`
	SubscriptionId   string `json:"subscriptionId"`
}
type TestSubscriptionPurchaseNotification struct {
	Version string `json:"version"`
}
type SubscriptionPurchaseNotification struct {
	Version                  string                                `json:"version"`
	PackageName              string                                `json:"packageName"`
	EventTimeMillis          string                                `json:"eventTimeMillis"`
	SubscriptionNotification *SubscriptionPurchaseNotification     `json:"subscriptionNotification"`
	TestNotification         *TestSubscriptionPurchaseNotification `json:"testNotification"`
}

// PubSubMessage is the payload of a Pub/Sub event. Please refer to the docs for
// additional information regarding Pub/Sub events.
type PubSubMessage struct {
	Data []byte `json:"data"`
}

func Redirect(ctx context.Context, m PubSubMessage) error {
	// . Init
	stag := os.Getenv("STAG")
	prod := os.Getenv("PROD")

	// . Get Pub/Sub message
	jsonRawData := m.Data
	log.Println("incoming message: ", string(jsonRawData))

	// . Packing new json for webhook service
	base64data := base64.StdEncoding.EncodeToString(jsonRawData)
	webhook := &WebhookReq{
		Message: &WebhookReqMessage{
			Data: base64data,
		},
	}

	webhookData, err := json.Marshal(&webhook)
	if err != nil {
		log.Println("error happens when marshalling WebhookReq object. Error: ", err)
	}

	// . Unmarshalling data
	subscriptionPurchaseNotification := &SubscriptionPurchaseNotification{}
	err = json.Unmarshal(jsonRawData, &subscriptionPurchaseNotification)
	if err != nil {
		log.Println("error happens when unmarshalling data to SubscriptionPurchaseNotification object. Error: ", err)
	}

	// . Check for test notification
	if subscriptionPurchaseNotification.TestNotification != nil {
		err = redirectRequest(stag, webhookData)
		if err != nil {
			log.Printf("redirect request failed: %s", err)
			return err
		}
		return nil
	}

	// . Init playstore client to verify incoming request
	client, err := playstore.New([]byte(jsonKey))
	if err != nil {
		log.Printf("request failed when init playstore client: %s", err)
		return err
	}
	if client == nil {
		log.Printf("request failed when init playstore client: %s", err)
		return err
	}

	resp, err := client.VerifySubscription(ctx,
		subscriptionPurchaseNotification.PackageName,
		subscriptionPurchaseNotification.SubscriptionNotification.SubscriptionId,
		subscriptionPurchaseNotification.SubscriptionNotification.PurchaseToken,
	)
	if err != nil {
		log.Printf("request failed when verify subscription: %s", err)
		return err
	}

	// . Check PurchaseType and redirect request
	purchaseType := resp.PurchaseType
	if purchaseType != nil {
		if null.Int64FromPtr(purchaseType).Int64 == int64(0) {
			err = redirectRequest(stag, webhookData)
		} else {
			err = redirectRequest(prod, webhookData)
		}
	} else {
		err = redirectRequest(prod, webhookData)
	}

	if err != nil {
		log.Printf("redirect request failed: %s", err)
		return err
	}

	return nil
}

func redirectRequest(url string, body []byte) error {
	defer log.Printf("Send json to %s \n%s ", url, string(body))
	proxyReq, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		log.Println("Failed to create request. Error: ", err)
		return err
	}

	netTransport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 5 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout: 5 * time.Second,
	}
	httpClient := &http.Client{
		Timeout:   time.Second * 10,
		Transport: netTransport,
	}

	resp, err := httpClient.Do(proxyReq)
	if err != nil {
		log.Println("Failed to send request. Error: ", err)
		return err
	}

	log.Println(resp)

	defer resp.Body.Close()

	return nil
}
