# Tunnel(Developing)

## Description

Tunnel that enables NAT traversal and QUIC support for building peer-to-peer (P2P) applications:

Tunnel is an open-source Golang package that provides Network Address Translation (NAT) traversal and Quick UDP Internet
Connections (QUIC) protocol functionality to streamline the development of decentralized, serverless P2P applications.

The core capabilities of Tunnel include:

NAT Traversal via UDP Hole Punching - Tunnel base the Session Traversal Utilities for NAT (STUN) protocol and
leverages birthday attacks to establish direct peer-to-peer connectivity through symmetric NAT devices.
Integration of the QUIC Protocol - Tunnel incorporates QUIC protocol wrappers that facilitate P2P communication with
encryption, authentication, congestion control, and connection migration.
Peer Discovery Mechanisms - Tunnel offers peer discovery APIs to locate other peers across the P2P network and exchange
connection information.
User-Friendly API - The GoHole API abstracts away the complexities of NAT traversal and QUIC. Developers can focus on
application logic while GoHole handles the underlying P2P communication.
Tunnel empowers developers to rapidly construct decentralized, serverless applications such as messaging, file sharing,
real-time communication, and more. By managing NAT traversal and transport-layer security, Tunnel streamlines P2P
development in Golang.

The networking boilerplate code is handled by Tunnel:

- UDP hole punching for NAT traversal
- QUIC encryption and multiplexing
- Peer discovery and info exchange
- Connection management, migration, and resilience

Tunnel is available on Github.

## Example

## Reference

- [Birthday Problem](https://en.wikipedia.org/wiki/Birthday_problem)
- [how-nat-traversal-works](https://tailscale.com/blog/how-nat-traversal-works/)