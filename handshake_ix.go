package nebula

import (
	"sync/atomic"
	"time"

	"github.com/flynn/noise"
	"github.com/golang/protobuf/proto"
)

// NOISE IX Handshakes

// This function constructs a handshake packet, but does not actually send it
// Sending is done by the handshake manager
func ixHandshakeStage0(f *Interface, vpnIp uint32, hostinfo *HostInfo) {
	// This queries the lighthouse if we don't know a remote for the host
	if hostinfo.remote == nil {
		ips, err := f.lightHouse.Query(vpnIp, f)
		if err != nil {
			//l.Debugln(err)
		}
		for _, ip := range ips {
			hostinfo.AddRemote(ip)
		}
	}

	err := f.handshakeManager.AddIndexHostInfo(hostinfo)
	if err != nil {
		f.l.WithError(err).WithField("vpnIp", IntIp(vpnIp)).
			WithField("handshake", m{"stage": 0, "style": "ix_psk0"}).Error("Failed to generate index")
		return
	}

	ci := hostinfo.ConnectionState

	hsProto := &NebulaHandshakeDetails{
		InitiatorIndex: hostinfo.localIndexId,
		Time:           uint64(time.Now().Unix()),
		Cert:           ci.certState.rawCertificateNoKey,
	}

	hsBytes := []byte{}

	hs := &NebulaHandshake{
		Details: hsProto,
	}
	hsBytes, err = proto.Marshal(hs)

	if err != nil {
		f.l.WithError(err).WithField("vpnIp", IntIp(vpnIp)).
			WithField("handshake", m{"stage": 0, "style": "ix_psk0"}).Error("Failed to marshal handshake message")
		return
	}

	header := HeaderEncode(make([]byte, HeaderLen), Version, uint8(handshake), handshakeIXPSK0, 0, 1)
	atomic.AddUint64(&ci.atomicMessageCounter, 1)

	msg, _, _, err := ci.H.WriteMessage(header, hsBytes)
	if err != nil {
		f.l.WithError(err).WithField("vpnIp", IntIp(vpnIp)).
			WithField("handshake", m{"stage": 0, "style": "ix_psk0"}).Error("Failed to call noise.WriteMessage")
		return
	}

	// We are sending handshake packet 1, so we don't expect to receive
	// handshake packet 1 from the responder
	ci.window.Update(f.l, 1)

	hostinfo.HandshakePacket[0] = msg
	hostinfo.HandshakeReady = true
	hostinfo.handshakeStart = time.Now()

}

func ixHandshakeStage1(f *Interface, addr *udpAddr, packet []byte, h *Header) {
	ci := f.newConnectionState(f.l, false, noise.HandshakeIX, []byte{}, 0)
	// Mark packet 1 as seen so it doesn't show up as missed
	ci.window.Update(f.l, 1)

	msg, _, _, err := ci.H.ReadMessage(nil, packet[HeaderLen:])
	if err != nil {
		f.l.WithError(err).WithField("udpAddr", addr).
			WithField("handshake", m{"stage": 1, "style": "ix_psk0"}).Error("Failed to call noise.ReadMessage")
		return
	}

	hs := &NebulaHandshake{}
	err = proto.Unmarshal(msg, hs)
	/*
		l.Debugln("GOT INDEX: ", hs.Details.InitiatorIndex)
	*/
	if err != nil || hs.Details == nil {
		f.l.WithError(err).WithField("udpAddr", addr).
			WithField("handshake", m{"stage": 1, "style": "ix_psk0"}).Error("Failed unmarshal handshake message")
		return
	}

	remoteCert, err := RecombineCertAndValidate(ci.H, hs.Details.Cert, f.caPool)
	if err != nil {
		f.l.WithError(err).WithField("udpAddr", addr).
			WithField("handshake", m{"stage": 1, "style": "ix_psk0"}).WithField("cert", remoteCert).
			Info("Invalid certificate from host")
		return
	}
	vpnIP := ip2int(remoteCert.Details.Ips[0].IP)
	certName := remoteCert.Details.Name
	fingerprint, _ := remoteCert.Sha256Sum()

	if vpnIP == ip2int(f.certState.certificate.Details.Ips[0].IP) {
		f.l.WithField("vpnIp", IntIp(vpnIP)).WithField("udpAddr", addr).
			WithField("certName", certName).
			WithField("fingerprint", fingerprint).
			WithField("handshake", m{"stage": 1, "style": "ix_psk0"}).Error("Refusing to handshake with myself")
		return
	}

	myIndex, err := generateIndex(f.l)
	if err != nil {
		f.l.WithError(err).WithField("vpnIp", IntIp(vpnIP)).WithField("udpAddr", addr).
			WithField("certName", certName).
			WithField("fingerprint", fingerprint).
			WithField("handshake", m{"stage": 1, "style": "ix_psk0"}).Error("Failed to generate index")
		return
	}

	hostinfo := &HostInfo{
		ConnectionState: ci,
		Remotes:         []*udpAddr{},
		localIndexId:    myIndex,
		remoteIndexId:   hs.Details.InitiatorIndex,
		hostId:          vpnIP,
		HandshakePacket: make(map[uint8][]byte, 0),
	}

	f.l.WithField("vpnIp", IntIp(vpnIP)).WithField("udpAddr", addr).
		WithField("certName", certName).
		WithField("fingerprint", fingerprint).
		WithField("initiatorIndex", hs.Details.InitiatorIndex).WithField("responderIndex", hs.Details.ResponderIndex).
		WithField("remoteIndex", h.RemoteIndex).WithField("handshake", m{"stage": 1, "style": "ix_psk0"}).
		Info("Handshake message received")

	hs.Details.ResponderIndex = myIndex
	hs.Details.Cert = ci.certState.rawCertificateNoKey

	hsBytes, err := proto.Marshal(hs)
	if err != nil {
		f.l.WithError(err).WithField("vpnIp", IntIp(hostinfo.hostId)).WithField("udpAddr", addr).
			WithField("certName", certName).
			WithField("fingerprint", fingerprint).
			WithField("handshake", m{"stage": 1, "style": "ix_psk0"}).Error("Failed to marshal handshake message")
		return
	}

	header := HeaderEncode(make([]byte, HeaderLen), Version, uint8(handshake), handshakeIXPSK0, hs.Details.InitiatorIndex, 2)
	msg, dKey, eKey, err := ci.H.WriteMessage(header, hsBytes)
	if err != nil {
		f.l.WithError(err).WithField("vpnIp", IntIp(hostinfo.hostId)).WithField("udpAddr", addr).
			WithField("certName", certName).
			WithField("fingerprint", fingerprint).
			WithField("handshake", m{"stage": 1, "style": "ix_psk0"}).Error("Failed to call noise.WriteMessage")
		return
	} else if dKey == nil || eKey == nil {
		f.l.WithField("vpnIp", IntIp(hostinfo.hostId)).WithField("udpAddr", addr).
			WithField("certName", certName).
			WithField("fingerprint", fingerprint).
			WithField("handshake", m{"stage": 1, "style": "ix_psk0"}).Error("Noise did not arrive at a key")
		return
	}

	hostinfo.HandshakePacket[0] = make([]byte, len(packet[HeaderLen:]))
	copy(hostinfo.HandshakePacket[0], packet[HeaderLen:])

	// Regardless of whether you are the sender or receiver, you should arrive here
	// and complete standing up the connection.
	hostinfo.HandshakePacket[2] = make([]byte, len(msg))
	copy(hostinfo.HandshakePacket[2], msg)

	// We are sending handshake packet 2, so we don't expect to receive
	// handshake packet 2 from the initiator.
	ci.window.Update(f.l, 2)

	ci.peerCert = remoteCert
	ci.dKey = NewNebulaCipherState(dKey)
	ci.eKey = NewNebulaCipherState(eKey)
	//l.Debugln("got symmetric pairs")

	//hostinfo.ClearRemotes()
	hostinfo.AddRemote(addr)
	hostinfo.ForcePromoteBest(f.hostMap.preferredRanges)
	hostinfo.CreateRemoteCIDR(remoteCert)

	hostinfo.Lock()
	defer hostinfo.Unlock()

	// Only overwrite existing record if we should win the handshake race
	overwrite := vpnIP > ip2int(f.certState.certificate.Details.Ips[0].IP)
	existing, err := f.handshakeManager.CheckAndComplete(hostinfo, 0, overwrite, f)
	if err != nil {
		switch err {
		case ErrAlreadySeen:
			msg = existing.HandshakePacket[2]
			f.messageMetrics.Tx(handshake, NebulaMessageSubType(msg[1]), 1)
			err := f.outside.WriteTo(msg, addr)
			if err != nil {
				f.l.WithField("vpnIp", IntIp(existing.hostId)).WithField("udpAddr", addr).
					WithField("handshake", m{"stage": 2, "style": "ix_psk0"}).WithField("cached", true).
					WithError(err).Error("Failed to send handshake message")
			} else {
				f.l.WithField("vpnIp", IntIp(existing.hostId)).WithField("udpAddr", addr).
					WithField("handshake", m{"stage": 2, "style": "ix_psk0"}).WithField("cached", true).
					Info("Handshake message sent")
			}
			return
		case ErrExistingHostInfo:
			// This means there was an existing tunnel and we didn't win
			// handshake avoidance
			f.l.WithField("vpnIp", IntIp(vpnIP)).WithField("udpAddr", addr).
				WithField("certName", certName).
				WithField("fingerprint", fingerprint).
				WithField("initiatorIndex", hs.Details.InitiatorIndex).WithField("responderIndex", hs.Details.ResponderIndex).
				WithField("remoteIndex", h.RemoteIndex).WithField("handshake", m{"stage": 1, "style": "ix_psk0"}).
				Info("Prevented a handshake race")

			// Send a test packet to trigger an authenticated tunnel test, this should suss out any lingering tunnel issues
			f.SendMessageToVpnIp(test, testRequest, vpnIP, []byte(""), make([]byte, 12, 12), make([]byte, mtu))
			return
		case ErrLocalIndexCollision:
			// This means we failed to insert because of collision on localIndexId. Just let the next handshake packet retry
			f.l.WithField("vpnIp", IntIp(vpnIP)).WithField("udpAddr", addr).
				WithField("certName", certName).
				WithField("fingerprint", fingerprint).
				WithField("initiatorIndex", hs.Details.InitiatorIndex).WithField("responderIndex", hs.Details.ResponderIndex).
				WithField("remoteIndex", h.RemoteIndex).WithField("handshake", m{"stage": 1, "style": "ix_psk0"}).
				WithField("localIndex", hostinfo.localIndexId).WithField("collision", IntIp(existing.hostId)).
				Error("Failed to add HostInfo due to localIndex collision")
			return
		default:
			// Shouldn't happen, but just in case someone adds a new error type to CheckAndComplete
			// And we forget to update it here
			f.l.WithError(err).WithField("vpnIp", IntIp(vpnIP)).WithField("udpAddr", addr).
				WithField("certName", certName).
				WithField("fingerprint", fingerprint).
				WithField("initiatorIndex", hs.Details.InitiatorIndex).WithField("responderIndex", hs.Details.ResponderIndex).
				WithField("remoteIndex", h.RemoteIndex).WithField("handshake", m{"stage": 1, "style": "ix_psk0"}).
				Error("Failed to add HostInfo to HostMap")
			return
		}
	}

	// Do the send
	f.messageMetrics.Tx(handshake, NebulaMessageSubType(msg[1]), 1)
	err = f.outside.WriteTo(msg, addr)
	if err != nil {
		f.l.WithField("vpnIp", IntIp(vpnIP)).WithField("udpAddr", addr).
			WithField("certName", certName).
			WithField("fingerprint", fingerprint).
			WithField("initiatorIndex", hs.Details.InitiatorIndex).WithField("responderIndex", hs.Details.ResponderIndex).
			WithField("remoteIndex", h.RemoteIndex).WithField("handshake", m{"stage": 2, "style": "ix_psk0"}).
			WithError(err).Error("Failed to send handshake")
	} else {
		f.l.WithField("vpnIp", IntIp(vpnIP)).WithField("udpAddr", addr).
			WithField("certName", certName).
			WithField("fingerprint", fingerprint).
			WithField("initiatorIndex", hs.Details.InitiatorIndex).WithField("responderIndex", hs.Details.ResponderIndex).
			WithField("remoteIndex", h.RemoteIndex).WithField("handshake", m{"stage": 2, "style": "ix_psk0"}).
			Info("Handshake message sent")
	}

	hostinfo.handshakeComplete(f.l)

	return
}

func ixHandshakeStage2(f *Interface, addr *udpAddr, hostinfo *HostInfo, packet []byte, h *Header) bool {
	if hostinfo == nil {
		// Nothing here to tear down, got a bogus stage 2 packet
		return true
	}

	hostinfo.Lock()
	defer hostinfo.Unlock()

	ci := hostinfo.ConnectionState
	if ci.ready {
		f.l.WithField("vpnIp", IntIp(hostinfo.hostId)).WithField("udpAddr", addr).
			WithField("handshake", m{"stage": 2, "style": "ix_psk0"}).WithField("header", h).
			Info("Handshake is already complete")

		// We already have a complete tunnel, there is nothing that can be done by processing further stage 1 packets
		return false
	}

	msg, eKey, dKey, err := ci.H.ReadMessage(nil, packet[HeaderLen:])
	if err != nil {
		f.l.WithError(err).WithField("vpnIp", IntIp(hostinfo.hostId)).WithField("udpAddr", addr).
			WithField("handshake", m{"stage": 2, "style": "ix_psk0"}).WithField("header", h).
			Error("Failed to call noise.ReadMessage")

		// We don't want to tear down the connection on a bad ReadMessage because it could be an attacker trying
		// to DOS us. Every other error condition after should to allow a possible good handshake to complete in the
		// near future
		return false
	} else if dKey == nil || eKey == nil {
		f.l.WithField("vpnIp", IntIp(hostinfo.hostId)).WithField("udpAddr", addr).
			WithField("handshake", m{"stage": 2, "style": "ix_psk0"}).
			Error("Noise did not arrive at a key")

		// This should be impossible in IX but just in case, if we get here then there is no chance to recover
		// the handshake state machine. Tear it down
		return true
	}

	hs := &NebulaHandshake{}
	err = proto.Unmarshal(msg, hs)
	if err != nil || hs.Details == nil {
		f.l.WithError(err).WithField("vpnIp", IntIp(hostinfo.hostId)).WithField("udpAddr", addr).
			WithField("handshake", m{"stage": 2, "style": "ix_psk0"}).Error("Failed unmarshal handshake message")

		// The handshake state machine is complete, if things break now there is no chance to recover. Tear down and start again
		return true
	}

	remoteCert, err := RecombineCertAndValidate(ci.H, hs.Details.Cert, f.caPool)
	if err != nil {
		f.l.WithError(err).WithField("vpnIp", IntIp(hostinfo.hostId)).WithField("udpAddr", addr).
			WithField("cert", remoteCert).WithField("handshake", m{"stage": 2, "style": "ix_psk0"}).
			Error("Invalid certificate from host")

		// The handshake state machine is complete, if things break now there is no chance to recover. Tear down and start again
		return true
	}

	vpnIP := ip2int(remoteCert.Details.Ips[0].IP)
	certName := remoteCert.Details.Name
	fingerprint, _ := remoteCert.Sha256Sum()

	if vpnIP != hostinfo.hostId {
		f.l.WithField("intendedVpnIp", IntIp(hostinfo.hostId)).WithField("haveVpnIp", IntIp(vpnIP)).
			WithField("udpAddr", addr).WithField("certName", certName).
			WithField("handshake", m{"stage": 2, "style": "ix_psk0"}).
			Info("Incorrect host responded to handshake")

		if ho, _ := f.handshakeManager.pendingHostMap.QueryVpnIP(vpnIP); ho != nil {
			// We might have a pending tunnel to this host already, clear out that attempt since we have a tunnel now
			f.handshakeManager.pendingHostMap.DeleteHostInfo(ho)
		}

		// Release our old handshake from pending, it should not continue
		f.handshakeManager.pendingHostMap.DeleteHostInfo(hostinfo)

		// Create a new hostinfo/handshake for the intended vpn ip
		//TODO: this adds it to the timer wheel in a way that aggressively retries
		newHostInfo := f.getOrHandshake(hostinfo.hostId)
		newHostInfo.Lock()

		// Block the current used address
		newHostInfo.unlockedBlockRemote(addr)

		// If this is an ongoing issue our previous hostmap will have some bad ips too
		for _, v := range hostinfo.badRemotes {
			newHostInfo.unlockedBlockRemote(v)
		}
		//TODO: this is me enabling tests
		newHostInfo.ForcePromoteBest(f.hostMap.preferredRanges)

		f.l.WithField("blockedUdpAddrs", newHostInfo.badRemotes).WithField("vpnIp", IntIp(vpnIP)).
			WithField("remotes", newHostInfo.Remotes).
			Info("Blocked addresses for handshakes")

		// Swap the packet store to benefit the original intended recipient
		newHostInfo.packetStore = hostinfo.packetStore
		hostinfo.packetStore = []*cachedPacket{}

		// Set the current hostId to the new vpnIp
		hostinfo.hostId = vpnIP
		newHostInfo.Unlock()
	}

	// Mark packet 2 as seen so it doesn't show up as missed
	ci.window.Update(f.l, 2)

	duration := time.Since(hostinfo.handshakeStart).Nanoseconds()
	f.l.WithField("vpnIp", IntIp(vpnIP)).WithField("udpAddr", addr).
		WithField("certName", certName).
		WithField("fingerprint", fingerprint).
		WithField("initiatorIndex", hs.Details.InitiatorIndex).WithField("responderIndex", hs.Details.ResponderIndex).
		WithField("remoteIndex", h.RemoteIndex).WithField("handshake", m{"stage": 2, "style": "ix_psk0"}).
		WithField("durationNs", duration).
		Info("Handshake message received")

	hostinfo.remoteIndexId = hs.Details.ResponderIndex
	hs.Details.Cert = ci.certState.rawCertificateNoKey

	// Store their cert and our symmetric keys
	ci.peerCert = remoteCert
	ci.dKey = NewNebulaCipherState(dKey)
	ci.eKey = NewNebulaCipherState(eKey)

	// Make sure the current udpAddr being used is set for responding
	hostinfo.SetRemote(addr)

	// Build up the radix for the firewall if we have subnets in the cert
	hostinfo.CreateRemoteCIDR(remoteCert)

	// Complete our handshake and update metrics, this will replace any existing tunnels for this vpnIp
	//TODO: Complete here does not do a race avoidance, it will just take the new tunnel. Is this ok?
	f.handshakeManager.Complete(hostinfo, f)
	hostinfo.handshakeComplete(f.l)
	f.metricHandshakes.Update(duration)

	return false
}
