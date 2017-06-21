package cluster

import (
	"github.com/hashicorp/memberlist"

	"common"
	"logger"
)

//
// Node notification hooks about members joining leaving and updating
//
type eventDelegate struct {
	c *Cluster
}

func (ed *eventDelegate) NotifyJoin(node *memberlist.Node) {
	ed.c.joinNode(node)
}

func (ed *eventDelegate) NotifyLeave(node *memberlist.Node) {
	ed.c.leaveNode(node)
}

func (ed *eventDelegate) NotifyUpdate(node *memberlist.Node) {
	ed.c.updateNode(node)
}

//
// Node notification hooks about members conflicting
//

type conflictDelegate struct {
	c *Cluster
}

func (cd *conflictDelegate) NotifyConflict(knownNode, otherNode *memberlist.Node) {
	cd.c.resolveNodeConflict(knownNode, otherNode)
}

//
// Gossip messaging handling on gateway message
//

type messageDelegate struct {
	c *Cluster
}

func (md *messageDelegate) NodeMeta(limit int) []byte {
	nodeTags, err := PackNodeTags(md.c.conf.NodeTags)
	if err != nil {
		logger.Errorf("[pack node tags failed: %v]", err)
		return make([]byte, 0)
	}

	if len(nodeTags) > limit {
		logger.Errorf("[tags of the node is too much]")
		return make([]byte, 0)
	}

	return nodeTags
}

func (md *messageDelegate) NotifyMsg(buff []byte) {
	if len(buff) == 0 {
		// defensive, nothing to do
		return
	}

	var messageQueue *memberlist.TransmitLimitedQueue
	forward := false

	msgType := messageType(buff[0])
	switch msgType {
	case memberJoinMessage:
		var msg messageMemberJoin
		err := Unpack(buff[1:], &msg)
		if err != nil {
			logger.Errorf("[unpack member join message failed: %v]", err)
			break
		}

		logger.Debugf("[received member join memssage from node %s at logical clock %d]",
			msg.NodeName, msg.JoinTime)

		messageQueue = md.c.memberMessageSendQueue
		forward = md.c.operateNodeJoin(&msg)
	case memberLeaveMessage:
		var msg messageMemberLeave
		err := Unpack(buff[1:], &msg)
		if err != nil {
			logger.Errorf("[unpack member leave message failed: %v]", err)
			break
		}

		logger.Debugf("[received member leave memssage from node %s at logical clock %d]",
			msg.NodeName, msg.LeaveTime)

		messageQueue = md.c.memberMessageSendQueue
		forward = md.c.operateNodeLeave(&msg)
	case requestMessage:
		var msg messageRequest
		err := Unpack(buff[1:], &msg)
		if err != nil {
			logger.Errorf("[unpack request message failed: %v]", err)
			break
		}

		logger.Debugf("[received request memssage from node %s at logical clock %d]",
			msg.RequestNodeName, msg.RequestTime)

		messageQueue = md.c.requestMessageSendQueue
		forward = md.c.operateRequest(&msg)
	case responseMessage:
		var msg messageResponse
		err := Unpack(buff[1:], &msg)
		if err != nil {
			logger.Errorf("[unpack response message failed: %v]", err)
			break
		}

		logger.Debugf("[received response memssage from node %s at logical clock %d]",
			msg.ResponseNodeName, msg.RequestTime)

		messageQueue = nil
		forward = md.c.operateResponse(&msg)
	case messageRelayMessage:
		var msg messageRelay
		err := Unpack(buff[1:], &msg)
		if err != nil {
			logger.Errorf("[unpack relay message failed: %v]", err)
			break
		}

		logger.Debugf("[received relay memssage from node %s]",
			msg.SourceNodeName)

		messageQueue = nil
		forward = md.c.operateRelay(&msg)
	default:
		logger.Errorf("[BUG: received unknown message type, ignored: %d]", msgType)
	}

	if forward {
		dup := make([]byte, len(buff))
		copy(dup, buff)
		fanoutBuffer(messageQueue, dup, nil)
	}
}

func (md *messageDelegate) GetBroadcasts(overhead, limit int) [][]byte {
	msgList := md.c.memberMessageSendQueue.GetBroadcasts(overhead, limit)

	size := overhead * len(msgList)
	for _, msg := range msgList {
		size += len(msg)
	}

	requestMessageList := md.c.requestMessageSendQueue.GetBroadcasts(overhead, limit-size)
	msgList = append(msgList, requestMessageList...)

	return msgList
}

func (d *messageDelegate) LocalState(join bool) []byte {
	d.c.membersLock.RLock()
	defer d.c.membersLock.RUnlock()

	msg := messagePushPull{
		MemberClockTime:        d.c.memberClock.Time(),
		RequestClockTime:       d.c.requestClock.Time(),
		MemberLastMessageTimes: make(map[string]logicalTime, len(d.c.members)),
	}

	for name, ms := range d.c.members {
		msg.MemberLastMessageTimes[name] = ms.lastMessageTime
	}

	msg.LeftMemberNames = append(msg.LeftMemberNames, d.c.leftMembers.names()...)

	buff, err := PackWithHeader(&msg, uint8(statePushPullMessage))
	if err != nil {
		logger.Errorf("[pack state push/pull message failed: %v]", err)
		return nil
	}

	logger.Debugf("[prepared local state push/pull message]")

	return buff
}

func (d *messageDelegate) MergeRemoteState(buff []byte, isJoin bool) {
	if len(buff) == 0 {
		// defensive, nothing to do
		return
	}

	msgType := messageType(buff[0])
	if msgType != statePushPullMessage {
		logger.Errorf("[BUG: received illegal state push/pull message, ignored: %d]", msgType)
	}

	msg := messagePushPull{}

	err := Unpack(buff[1:], &msg)
	if err != nil {
		logger.Errorf("[unpack state push/pull message failed: %v]", err)
		return
	}

	logger.Debugf("[received state push/pull memssage]")
	if msg.MemberClockTime > 0 {
		d.c.memberClock.Update(msg.MemberClockTime - 1)
	}

	if msg.RequestClockTime > 0 {
		d.c.requestClock.Update(msg.RequestClockTime - 1)
	}

	var leftMemberNames []string

	for _, name := range msg.LeftMemberNames {
		leftMemberNames = append(leftMemberNames, name)

		d.c.operateNodeLeave(&messageMemberLeave{
			LeaveTime: msg.MemberLastMessageTimes[name],
			NodeName:  name,
		})
	}

	for name, lastMessageTime := range msg.MemberLastMessageTimes {
		if !common.StrInSlice(name, leftMemberNames) {
			d.c.operateNodeJoin(&messageMemberJoin{
				JoinTime: lastMessageTime,
				NodeName: name,
			})
		}
	}
}
