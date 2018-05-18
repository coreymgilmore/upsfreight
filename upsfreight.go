/*Package upsfreight provides tooling to connect to the UPS Freight API.  This is for truck shipments,
not small parcels.  Think LTL (less than truckload) shipments.  This code was created off the UPS API
documentation.  This uses UPS's JSON API.

You will need to have a UPS account and register for API access to use this code.

Currently this package can perform:
	- pickup requests
*/

package upsfreight

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/pkg/errors"
)

//api urls
const (
	upsTestURL       = "https://wwwcie.ups.com/rest/FreightPickup"
	upsProductionURL = "https://onlinetools.ups.com/rest/FreightPickup"
)

//upsURL is set to the test URL by default
//This is changed to the production URL when the SetProductionMode function is called
//Forcing the developer to call the SetProductionMode function ensures the production URL is only used
//when actually needed.
var upsURL = upsTestURL

//PickupRequest is the main container struct for data sent to UPS to request a pickup
//This format, and children types, was determined from UPS API documentation.
type PickupRequest struct {
	Security             security
	FreightPickupRequest PickupRequestDetails
}

//security is the authentication for the request
//This has two pieces, your UPS website login credential and the API acccess key
type security struct {
	UsernameToken struct {
		Username string //ups website login username
		Password string //ups website login password
	}
	UPSServiceAccessToken struct {
		AccessLicenseNumber string //api access key from ups
	}
}

//PickupRequestDetails is the container around the actual pickup request
//This holds the ship to location, who is making the pickup request, the ship from location,
//the shipment details, and other pickup information
type PickupRequestDetails struct {
	Request struct {
		TransactionReference struct {
			CustomerContext string //some unique identifier, time stamp or somethine else unique
		}
	}

	AdditionalComments     string
	DestinationPostalCode  string          //the ship to location
	DestinationCountryCode string          //the ship to location
	Requester              Requester       //who is scheduling the pickup
	ShipFrom               ShipFromAddress //the ship from location
	ShipmentDetail         ShipmentDetail  //what is shipping
	PickupDate             string          //YYYYMMDD; cannot be in the past
	EarliestTimeReady      string          //24 hour time, HHMM; cannot be in the past
	LatestTimeReady        string          //24 hour time, HHMM; cannot be in the past
}

//Requester is data on who is scheduling the pickup
type Requester struct {
	AttentionName string //a person's name or department name
	EMailAddress  string //for sending pickup request confirmation
	Name          string //company name where pickup is being made
	Phone         PhoneNum
}

//ShipFromAddress is the info on where the shipment is shipping from
type ShipFromAddress struct {
	AttentionName string  //a person's name or department name
	Name          string  //company name where pickup is being made
	Address       Address //the address where the pickup will be made
	Phone         PhoneNum
}

//PhoneNum is the container for a phone number
type PhoneNum struct {
	Number string
}

//Address is the container for an address
type Address struct {
	AddressLine       string //street
	City              string
	StateProvinceCode string //two characters
	PostalCode        string
	CountryCode       string //two characters
}

//ShipmentDetail holds data on the shipment
type ShipmentDetail struct {
	HazMatIndicator        string //usually blank
	PackagingType          PackagingType
	NumberOfPieces         string //must be a string for api to work
	DescriptionOfCommodity string
	Weight                 Weight
}

//PackagingType holds data on what format a shipment is in
//Skid, boxes, etc.
//Code is a three character code.  This can be found in the UPS API documentation.
//Description is the maching text description to the Code.
//Ex: Shipping a skid would be Code: SKD, Description: Skid
type PackagingType struct {
	Code        string
	Description string
}

//Weight holds data on the weight of the shipment
type Weight struct {
	UnitOfMeasurement struct {
		Code        string //LBS
		Description string //Pounds
	}
	Value string //must be a string for api to work; the actual weight, up to two decimal places
}

//PickupRequestResponse is the data we get back when a pickup is scheduled successfully
type PickupRequestResponse struct {
	FreightPickupResponse struct {
		Response struct {
			ResponseStatus struct {
				Code        string
				Description string
			}
			TransactionReference struct {
				CustomerContext string
			}
		}
		PickupRequestConfirmationNumber string
	}
}

//PickupRequestError is the data we get back from a pickup request when there is an error
type PickupRequestError struct {
	Fault struct {
		FaultCode   string `json:"faultcode"`
		FaultString string `json:"faultstring"`
		Detail      struct {
			Errors struct {
				ErrorDetail []errorDetail
			}
		} `json:"detail"`
	}
}

//errorDetail is the actual error message being returned from UPS.
//an error response can have one or more errorDetails
type errorDetail struct {
	Severity         string
	PrimaryErrorCode struct {
		Code        string
		Description string
	}
}

//apiCredentials is the log in information we will use to make pickup requests
//this variable is filled by the SetCredentials() func
var apiCredentials security

//SetCredentials saves the login credentials for the UPS website and API so we can make
//requests
func SetCredentials(username, password, accessKey string) {
	//web login
	apiCredentials.UsernameToken.Username = username
	apiCredentials.UsernameToken.Password = password

	//api access key
	apiCredentials.UPSServiceAccessToken.AccessLicenseNumber = accessKey

	return
}

//SetProductionMode chooses the production url for use
func SetProductionMode(yes bool) {
	upsURL = upsProductionURL
	return
}

//SetCustomerContext saves the unique identifier for this request to the request details
func (prd *PickupRequestDetails) SetCustomerContext(c string) {
	prd.Request.TransactionReference.CustomerContext = c
	return
}

//SetPickupSchedule sets the date and time range for a pickup
//This is the time UPS will attempt to perform the pickup
//Times should be in the future and be on the same date.
func (prd *PickupRequestDetails) SetPickupSchedule(startTime, endTime time.Time) error {
	//get date from times and make sure they are the same
	startYear, startMonth, startDay := startTime.Date()
	endYear, endMonth, endDay := endTime.Date()

	if (startYear != endYear) || (startMonth != endMonth) || (startDay != endDay) {
		return errors.New("upsfreight.SetPickupSchedule - startTime and endTime not same date")
	}

	//make sure start time is in the future
	now := time.Now()
	if startTime.Sub(now) < 0 {
		return errors.New("upsfreight.SetPickupSchedule - startTime is in the past")
	}

	//make sure end time is after start time
	//ups also requires a 2 hour window
	if startTime.Sub(now) < time.Hour*2 {
		return errors.New("upsfreight.SetPickupSchedule - endTime must be at least 2 hours after start time")
	}

	//save date and times
	prd.PickupDate = startTime.Format("20060102")
	prd.EarliestTimeReady = startTime.Format("1504")
	prd.LatestTimeReady = endTime.Format("1504")
	return nil
}

//RequestPickup performs the call the the UPS API to schedule a pickup
func (prd *PickupRequestDetails) RequestPickup() (responseData PickupRequestResponse, err error) {
	//build the PickupRequest struct
	pickupRequest := PickupRequest{
		Security:             apiCredentials,
		FreightPickupRequest: *prd,
	}

	//set measure of weight
	pickupRequest.FreightPickupRequest.ShipmentDetail.Weight.UnitOfMeasurement.Code = "LBS"
	pickupRequest.FreightPickupRequest.ShipmentDetail.Weight.UnitOfMeasurement.Description = "Pounds"

	//convert the struct to json
	jsonBytes, err := json.Marshal(pickupRequest)
	if err != nil {
		err = errors.Wrap(err, "upsfreight.RequestPickup - could not marshal json")
		return
	}

	//make the call the UPS
	//set a timeout since golang doesn't set one by default
	//we don't want this call to hang for too long
	timeout := time.Duration(7 * time.Second)
	httpClient := http.Client{
		Timeout: timeout,
	}
	res, err := httpClient.Post(upsURL, "application/json", bytes.NewReader(jsonBytes))
	if err != nil {
		errors.Wrap(err, "upsfreight.RequestPickup - could not make post request")
		return
	}

	//read the response
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		errors.Wrap(err, "upsfreight.RequestPickup - could not read response")
		return
	}

	err = json.Unmarshal(body, &responseData)
	if err != nil {
		errors.Wrap(err, "upsfreight.RequestPickup - could not unmarshal response")
		return
	}

	//check if data was returned meaning request was successful
	//if not, reread the response data and log it
	if responseData.FreightPickupResponse.PickupRequestConfirmationNumber == "" {
		log.Println("upsfreight.RequestPickup - pickup request failed")

		var errorData map[string]interface{}
		json.Unmarshal(body, &errorData)
		log.Printf("%+v", errorData)
		err = errors.New("upsfreight.RequestPickup - pickup request failed")
		return
	}

	//pickup request successful
	//response data will have confirmation number
	//an email should also have been sent to the requester email
	return
}
