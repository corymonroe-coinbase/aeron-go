package cluster

import (
	"bytes"
	"fmt"

	"github.com/corymonroe-coinbase/aeron-go/aeron"
	"github.com/corymonroe-coinbase/aeron-go/aeron/atomic"
	"github.com/corymonroe-coinbase/aeron-go/aeron/logbuffer"
	"github.com/corymonroe-coinbase/aeron-go/cluster/codecs"
)

type BoundedLogAdapter struct {
	marshaller     *codecs.SbeGoMarshaller
	options        *Options
	agent          *ClusteredServiceAgent
	image          *aeron.Image
	maxLogPosition int64
}

func (adapter *BoundedLogAdapter) IsDone() bool {
	return adapter.image.Position() >= adapter.maxLogPosition ||
		adapter.image.IsEndOfStream() ||
		adapter.image.IsClosed()
}

func (adapter *BoundedLogAdapter) Poll(limitPos int64) int {
	return adapter.image.BoundedPoll(adapter.onFragment, limitPos, adapter.options.LogFragmentLimit)
}

func (adapter *BoundedLogAdapter) onFragment(
	buffer *atomic.Buffer,
	offset int32,
	length int32,
	header *logbuffer.Header,
) {
	var hdr codecs.SbeGoMessageHeader
	buf := &bytes.Buffer{}
	buffer.WriteBytes(buf, offset, length)

	if err := hdr.Decode(adapter.marshaller, buf); err != nil {
		fmt.Println("BoundedLogAdaptor - header decode error: ", err)
	}
	if hdr.SchemaId != clusterSchemaId {
		fmt.Println("BoundedLogAdaptor - unexpected schemaId: ", hdr)
		return
	}

	switch hdr.TemplateId {
	case timerEventTemplateId:
		fmt.Println("BoundedLogAdaptor - got timer event")
	case sessionOpenTemplateId:
		event := &codecs.SessionOpenEvent{}
		if err := event.Decode(
			adapter.marshaller,
			buf,
			hdr.Version,
			hdr.BlockLength,
			adapter.options.RangeChecking,
		); err != nil {
			fmt.Println("session open decode error: ", err)
			return
		}

		adapter.agent.onSessionOpen(
			event.LeadershipTermId,
			header.Position(),
			event.ClusterSessionId,
			event.Timestamp,
			event.ResponseStreamId,
			string(event.ResponseChannel),
			event.EncodedPrincipal,
		)
	case sessionCloseTemplateId:
		event := &codecs.SessionCloseEvent{}
		if err := event.Decode(
			adapter.marshaller,
			buf,
			hdr.Version,
			hdr.BlockLength,
			adapter.options.RangeChecking,
		); err != nil {
			fmt.Println("session close decode error: ", err)
			return
		}

		adapter.agent.onSessionClose(
			event.LeadershipTermId,
			header.Position(),
			event.ClusterSessionId,
			event.Timestamp,
			event.CloseReason,
		)
	case clusterActionReqTemplateId:
		e := &codecs.ClusterActionRequest{}
		if err := e.Decode(adapter.marshaller, buf, hdr.Version, hdr.BlockLength, adapter.options.RangeChecking); err != nil {
			fmt.Println("cluster action request decode error: ", err)
		} else {
			adapter.agent.onServiceAction(e.LeadershipTermId, e.LogPosition, e.Timestamp, e.Action)
		}
	case newLeadershipTermTemplateId:
		e := &codecs.NewLeadershipTermEvent{}
		if err := e.Decode(adapter.marshaller, buf, hdr.Version, hdr.BlockLength, adapter.options.RangeChecking); err != nil {
			fmt.Println("new leadership term decode error: ", err)
		} else {
			//fmt.Println("BoundedLogAdaptor - got new leadership term: ", e)
			adapter.agent.onNewLeadershipTermEvent(e.LeadershipTermId, e.LogPosition, e.Timestamp, e.TermBaseLogPosition,
				e.LeaderMemberId, e.LogSessionId, e.TimeUnit, e.AppVersion)
		}
	case membershipChangeTemplateId:
		e := &codecs.MembershipChangeEvent{}
		if err := e.Decode(adapter.marshaller, buf, hdr.Version, hdr.BlockLength, adapter.options.RangeChecking); err != nil {
			fmt.Println("membership change event decode error: ", err)
		} else {
			fmt.Println("BoundedLogAdaptor - got membership change event: ", e)
		}
	case sessionMessageHeaderTemplateId:
		e := &codecs.SessionMessageHeader{}
		if err := e.Decode(adapter.marshaller, buf, hdr.Version, hdr.BlockLength, adapter.options.RangeChecking); err != nil {
			fmt.Println("session message header decode error: ", err)
		} else {
			adapter.agent.onSessionMessage(
				header.Position(),
				e.ClusterSessionId,
				e.Timestamp,
				buffer,
				offset+SessionMessageHeaderLength,
				length-SessionMessageHeaderLength,
				header,
			)
		}
	default:
		fmt.Println("BoundedLogAdaptor: unexpected template id: ", hdr.TemplateId)
	}
}

func (adapter *BoundedLogAdapter) Close() error {
	var err error
	if adapter.image != nil {
		err = adapter.image.Close()
		adapter.image = nil
	}
	return err
}
