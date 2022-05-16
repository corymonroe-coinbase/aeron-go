/*
Copyright 2016 Stanislav Liberman

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package aeron

import (
	"strings"

	"github.com/corymonroe-coinbase/aeron-go/aeron/atomic"
	"github.com/corymonroe-coinbase/aeron-go/aeron/logbuffer/term"
)

const (
	ChannelStatusErrored      = -1 // Channel has errored. Check logs for information
	ChannelStatusInitializing = 0  // Channel is being initialized
	ChannelStatusActive       = 1  // Channel has finished initialization and is active
	ChannelStatusClosing      = 2  // Channel is being closed
)

const CounterTypeDriverLocalSocketAddress = 14

// Subscription is the object responsible for receiving messages from media driver. It is specific to a channel and
// stream ID combination.
type Subscription struct {
	conductor       *ClientConductor
	channel         string
	roundRobinIndex int
	registrationID  int64
	streamID        int32
	channelStatusID int32

	images *ImageList

	isClosed atomic.Bool
}

// NewSubscription is a factory method to create new subscription to be added to the media driver
func NewSubscription(conductor *ClientConductor, channel string, registrationID int64, streamID, channelStatusID int32) *Subscription {
	sub := new(Subscription)
	sub.images = NewImageList()
	sub.conductor = conductor
	sub.channel = channel
	sub.registrationID = registrationID
	sub.streamID = streamID
	sub.channelStatusID = channelStatusID
	sub.roundRobinIndex = 0
	sub.isClosed.Set(false)

	return sub
}

// Channel returns the media address for delivery to the channel.
func (sub *Subscription) Channel() string {
	return sub.channel
}

// StreamID returns Stream identity for scoping within the channel media address.
func (sub *Subscription) StreamID() int32 {
	return sub.streamID
}

// IsClosed returns whether this subscription has been closed.
func (sub *Subscription) IsClosed() bool {
	return sub.isClosed.Get()
}

// ChannelStatus returns the status of the media channel for this Subscription.
// The status will be ChannelStatusErrored if a socket exception on setup or ChannelStatusActive if all is well.
func (sub *Subscription) ChannelStatus() int {
	if sub.IsClosed() {
		return -2
	}
	return int(sub.conductor.counterReader.GetCounterValue(sub.channelStatusID))
}

// ChannelStatusId returns the counter ID used to represent the channel status of this Subscription.
func (sub *Subscription) ChannelStatusId() int32 {
	return sub.channelStatusID
}

// Close will release all images in this subscription, send command to the driver and block waiting for response from
// the media driver. Images will be lingered by the ClientConductor.
func (sub *Subscription) Close() error {
	if sub.isClosed.CompareAndSet(false, true) {
		images := sub.images.Empty()
		sub.conductor.releaseSubscription(sub.registrationID, images)
	}

	return nil
}

// Poll is the primary receive mechanism on subscription.
func (sub *Subscription) Poll(handler term.FragmentHandler, fragmentLimit int) int {

	img := sub.images.Get()
	length := len(img)
	var fragmentsRead int

	if length > 0 {
		startingIndex := sub.roundRobinIndex
		sub.roundRobinIndex++
		if startingIndex >= length {
			sub.roundRobinIndex = 0
			startingIndex = 0
		}

		for i := startingIndex; i < length && fragmentsRead < fragmentLimit; i++ {
			fragmentsRead += img[i].Poll(handler, fragmentLimit-fragmentsRead)
		}

		for i := 0; i < startingIndex && fragmentsRead < fragmentLimit; i++ {
			fragmentsRead += img[i].Poll(handler, fragmentLimit-fragmentsRead)
		}
	}

	return fragmentsRead
}

// PollWithContext as for Poll() but provides an integer argument for passing contextual information
func (sub *Subscription) PollWithContext(handler term.FragmentHandler, fragmentLimit int) int {

	img := sub.images.Get()
	length := len(img)
	var fragmentsRead int

	if length > 0 {
		startingIndex := sub.roundRobinIndex
		sub.roundRobinIndex++
		if startingIndex >= length {
			sub.roundRobinIndex = 0
			startingIndex = 0
		}

		for i := startingIndex; i < length && fragmentsRead < fragmentLimit; i++ {
			fragmentsRead += img[i].PollWithContext(handler, fragmentLimit-fragmentsRead)
		}

		for i := 0; i < startingIndex && fragmentsRead < fragmentLimit; i++ {
			fragmentsRead += img[i].PollWithContext(handler, fragmentLimit-fragmentsRead)
		}
	}

	return fragmentsRead
}

func (sub *Subscription) hasImage(sessionID int32) bool {
	img := sub.images.Get()
	for _, image := range img {
		if image.sessionID == sessionID {
			return true
		}
	}
	return false
}

func (sub *Subscription) addImage(image *Image) *[]Image {

	images := sub.images.Get()

	sub.images.Set(append(images, *image))

	return &images
}

func (sub *Subscription) removeImage(correlationID int64) *Image {

	img := sub.images.Get()
	for ix, image := range img {
		if image.correlationID == correlationID {
			logger.Debugf("Removing image %v for subscription %d", image, sub.registrationID)

			img[ix] = img[len(img)-1]
			img = img[:len(img)-1]

			sub.images.Set(img)

			return &image
		}
	}
	return nil
}

// RegistrationID returns the registration id.
func (sub *Subscription) RegistrationID() int64 {
	return sub.registrationID
}

// IsConnected returns if this subscription is connected by having at least one open publication Image.
func (sub *Subscription) IsConnected() bool {
	for _, image := range sub.images.Get() {
		if !image.IsClosed() {
			return true
		}
	}
	return false
}

// HasImages is a helper method checking whether this subscription has any images associated with it.
func (sub *Subscription) HasImages() bool {
	images := sub.images.Get()
	return len(images) > 0
}

// ImageCount count of images associated with this subscription.
func (sub *Subscription) ImageCount() int {
	images := sub.images.Get()
	return len(images)
}

// ImageBySessionId returns the associated with the given sessionId.
func (sub *Subscription) ImageBySessionID(sessionID int32) *Image {
	img := sub.images.Get()
	for _, image := range img {
		if image.sessionID == sessionID {
			return &image
		}
	}
	return nil
}

// TryResolveChannelEndpointPort resolves the channel endpoint and replaces it with the port from the
// ephemeral range when 0 was provided. If there are no addresses, or if there is more than one, returned from
// LocalSocketAddresses() then the original channel is returned.
// If the channel is not ACTIVE, then empty string will be returned.
func (sub *Subscription) TryResolveChannelEndpointPort() string {
	if sub.ChannelStatus() != ChannelStatusActive {
		return ""
	}
	localSocketAddresses := sub.LocalSocketAddresses()
	if len(localSocketAddresses) != 1 {
		return sub.channel
	}
	uri, err := ParseChannelUri(sub.channel)
	if err != nil {
		logger.Warningf("error parsing channel (%s): %v", sub.channel, err)
		return sub.channel
	}
	endpoint := uri.Get("endpoint")
	if strings.HasSuffix(endpoint, ":0") {
		resolvedEndpoint := localSocketAddresses[0]
		i := strings.LastIndex(resolvedEndpoint, ":")
		uri.Set("endpoint", endpoint[:(len(endpoint)-2)]+resolvedEndpoint[i:])
		return uri.String()
	}
	return sub.channel
}

// LocalSocketAddresses fetches the local socket addresses for this subscription.
func (sub *Subscription) LocalSocketAddresses() []string {
	if sub.ChannelStatus() != ChannelStatusActive {
		return nil
	}
	var bindings []string
	reader := sub.conductor.counterReader
	reader.ScanForType(CounterTypeDriverLocalSocketAddress, func(counterId int32, keyBuffer *atomic.Buffer) {
		channelStatusId := keyBuffer.GetInt32(0)
		length := keyBuffer.GetInt32(4)
		if channelStatusId == sub.channelStatusID && length > 0 && reader.GetCounterValue(counterId) == ChannelStatusActive {
			bindings = append(bindings, string(keyBuffer.GetBytesArray(8, length)))
		}
	})
	return bindings
}

// IsConnectedTo is a helper function used primarily by tests, which is used within the same process to verify that
// subscription is connected to a specific publication.
func IsConnectedTo(sub *Subscription, pub *Publication) bool {
	img := sub.images.Get()
	if sub.channel == pub.channel && sub.streamID == pub.streamID {
		for _, image := range img {
			if image.sessionID == pub.sessionID {
				return true
			}
		}
	}

	return false
}

// ChannelStatusString provides a convenience method for logging and error handling
func ChannelStatusString(channelStatus int) string {
	switch channelStatus {
	case ChannelStatusErrored:
		return "ChannelStatusErrored"
	case ChannelStatusInitializing:
		return "ChannelStatusInitializing"
	case ChannelStatusActive:
		return "ChannelStatusActive"
	case ChannelStatusClosing:
		return "ChannelStatusClosing"
	default:
		return "Unknown"
	}
}
