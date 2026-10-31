EU range scope: OPC UA is background only. This package does not model or expose an OPC UA service or endpoint. Its station path uses IEC 104 northbound and Modbus TCP southbound; the historian receives CC snapshots over HTTP, not OPC UA. Do not treat this article as a description of a service available in the range.

### What OPC UA provides

OPC UA (OPC Unified Architecture) is an industrial communications standard used to exchange data and operations between software and automation systems. Unlike a single register map, its information model is an address space: typed nodes connected by references. A server can expose variables with attributes such as data type, engineering units and access rights, as well as objects and methods. Clients can browse that model, then read or write permitted values or call permitted methods.

The model can make an interface more descriptive, but it does not make every server self-explanatory. Meaning and organization still depend on what the server implements, its vendor model and any applicable companion specification. A familiar node name is not proof that the value is fresh, physically correct or safe to write.

### Communication patterns and integration

Client/server is a common OPC UA pattern: a client connects to a server, browses its address space and requests data or operations. Subscriptions can deliver monitored-item updates without a client repeatedly issuing each read. OPC UA PubSub is a separate communication pattern for distributing configured data sets; it is not the same thing as browsing a server's address space.

OPC UA appears in many kinds of industrial integration, including controller-to-supervisory systems and exchange with manufacturing or analytics systems. A deployment may place an OPC UA server, client or intermediary near an IT/OT boundary, but the role and data direction must be established from that deployment's design. Do not assume that a historian necessarily uses OPC UA, or that an integration endpoint should be reachable from an enterprise network merely because it carries telemetry.

Transport and endpoint details are deployment-specific. A UA TCP endpoint commonly uses TCP/4840, but ports can differ and other mappings exist. Confirm the configured endpoint and required direction before proposing a conduit; an open port alone does not identify the application's authorized operations.

### Security is configured, not automatic

OPC UA defines security mechanisms including application certificates, secure-channel security policies and message signing or encryption. User identity tokens and server-side authorization are distinct from application authentication: a trusted client certificate does not automatically grant every user or client permission to browse, read, write or call every node. Administrators must configure trust, certificate lifecycle, endpoint security modes and least-privilege access. A server configured with weak or absent protections does not become secure simply by speaking OPC UA.

Network segmentation remains useful for limiting which systems can reach an endpoint, but an ordinary L4 firewall rule distinguishes addresses, ports and transport connections, not OPC UA node-level rights or individual Read, Write and Call requests. In this EU range, the reference policy enforces L4 boundaries with DPI disabled; it does not demonstrate OPC UA inspection or service-level filtering. See [port and source boundaries](#eu-port-source-boundary) and [policy comparison](#eu-policy-comparison).

### Contrast with the EU station paths

The implemented station gateway is an IEC 104 server to the CC and polls field devices using Modbus TCP. Those protocols use their own point/register mappings and quality or freshness rules; see the [IEC 104 wire model](#iec104-wire-model) and [southbound Modbus map](#eu-southbound-modbus). The [historian](#eu-historian) receives the CC's snapshots through an HTTP ingest API and preserves observation provenance. None of these paths is an OPC UA endpoint, and decoding one protocol does not provide inspection or enforcement for another.

### Design questions for a real deployment

Before introducing OPC UA across a boundary, identify the server, clients, data model, data direction, required operations and owners. Record the exact endpoint and transport; verify certificate trust and renewal; limit which clients can connect; restrict user and node permissions; and retain suitable connection, authorization and change evidence. Test both allowed and denied paths with the service actually deployed. Keep the data consumer separate from control authority unless the operational design explicitly requires otherwise.
