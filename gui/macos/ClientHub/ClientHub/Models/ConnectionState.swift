import Foundation

enum ConnectionStatus: Int32 {
    case disconnected = 0
    case connecting = 1
    case connected = 2
    case reconnecting = 3

    var label: String {
        switch self {
        case .disconnected: return "Disconnected"
        case .connecting:   return "Connecting..."
        case .connected:    return "Connected"
        case .reconnecting: return "Reconnecting..."
        }
    }

    var color: String {
        switch self {
        case .disconnected: return "red"
        case .connecting:   return "yellow"
        case .connected:    return "green"
        case .reconnecting: return "yellow"
        }
    }
}

/// Holds the state for the client connection tab.
@MainActor
class ConnectionState: ObservableObject {
    @Published var serverAddr: String = ""
    @Published var clientName: String = ""
    @Published var secret: String = ""
    @Published var status: ConnectionStatus = .disconnected
    @Published var statusDetail: String = ""

    @Published var exposeServices: [ExposeServiceItem] = []
    @Published var forwards: [ForwardItem] = []

    var clientHandle: Int32 = -1

    private let lib = LibClientHub.shared

    var isRunning: Bool {
        status == .connecting || status == .connected || status == .reconnecting
    }

    func connect() {
        guard !isRunning else { return }

        var config: [String: Any] = [
            "server_addr": serverAddr,
            "client_name": clientName,
            "secret": secret,
        ]

        if !exposeServices.isEmpty {
            config["expose"] = exposeServices.map { svc in
                [
                    "name": svc.name,
                    "local_addr": svc.localAddr,
                    "protocol": svc.protocol,
                ] as [String: String]
            }
        }

        if !forwards.isEmpty {
            config["forward"] = forwards.map { fwd in
                [
                    "remote_client": fwd.remoteClient,
                    "remote_service": fwd.remoteService,
                    "listen_addr": fwd.listenAddr,
                    "protocol": fwd.protocol,
                ] as [String: String]
            }
        }

        guard let jsonData = try? JSONSerialization.data(withJSONObject: config),
              let jsonStr = String(data: jsonData, encoding: .utf8) else {
            statusDetail = "Failed to encode config"
            return
        }

        let handle = lib.clientCreate(configJSON: jsonStr)
        if handle < 0 {
            statusDetail = "Failed to create client"
            return
        }
        clientHandle = handle
        lib.clientStart(handle: handle)
    }

    func disconnect() {
        guard clientHandle >= 0 else { return }
        lib.clientStop(handle: clientHandle)
        lib.clientDestroy(handle: clientHandle)
        clientHandle = -1
        status = .disconnected
        statusDetail = "Disconnected by user"
    }
}

struct ExposeServiceItem: Identifiable, Hashable {
    let id = UUID()
    var name: String = ""
    var localAddr: String = ""
    var `protocol`: String = "tcp"
}

struct ForwardItem: Identifiable, Hashable {
    let id = UUID()
    var remoteClient: String = ""
    var remoteService: String = ""
    var listenAddr: String = ""
    var `protocol`: String = "tcp"
}
