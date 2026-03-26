import Foundation

/// Holds the state for the manager (admin) tab.
@MainActor
class ServerManager: ObservableObject {
    @Published var adminAddr: String = "127.0.0.1:7902"
    @Published var secret: String = ""
    @Published var isConnected: Bool = false
    @Published var errorMessage: String?

    @Published var clients: [ClientInfoItem] = []
    @Published var tunnels: [TunnelInfoItem] = []
    @Published var forwards: [ForwardInfoItem] = []
    @Published var serverStatus: ServerStatusInfo?

    var managerHandle: Int32 = -1

    private let lib = LibClientHub.shared

    func connectManager() {
        if managerHandle >= 0 {
            lib.managerDestroy(handle: managerHandle)
        }
        let handle = lib.managerCreate(addr: adminAddr, secret: secret)
        if handle < 0 {
            errorMessage = "Failed to create manager connection"
            isConnected = false
            return
        }
        managerHandle = handle
        isConnected = true
        errorMessage = nil
    }

    func disconnectManager() {
        if managerHandle >= 0 {
            lib.managerDestroy(handle: managerHandle)
            managerHandle = -1
        }
        isConnected = false
        clients = []
        tunnels = []
        forwards = []
        serverStatus = nil
    }

    func refreshAll() {
        fetchClients()
        fetchTunnels()
        fetchForwards()
        fetchStatus()
    }

    func fetchClients() {
        guard managerHandle >= 0 else { return }
        guard let resp = lib.managerListClients(handle: managerHandle) else { return }
        guard resp["success"] as? Bool != false else {
            errorMessage = resp["message"] as? String
            return
        }
        errorMessage = nil
        if let data = resp["data"] as? [[String: Any]] {
            clients = data.map { ClientInfoItem(from: $0) }
        }
    }

    func fetchTunnels() {
        guard managerHandle >= 0 else { return }
        guard let resp = lib.managerListTunnels(handle: managerHandle) else { return }
        guard resp["success"] as? Bool != false else {
            errorMessage = resp["message"] as? String
            return
        }
        if let data = resp["data"] as? [[String: Any]] {
            tunnels = data.map { TunnelInfoItem(from: $0) }
        }
    }

    func fetchForwards() {
        guard managerHandle >= 0 else { return }
        guard let resp = lib.managerListForwards(handle: managerHandle) else { return }
        guard resp["success"] as? Bool != false else {
            errorMessage = resp["message"] as? String
            return
        }
        if let data = resp["data"] as? [[String: Any]] {
            forwards = data.map { ForwardInfoItem(from: $0) }
        }
    }

    func fetchStatus() {
        guard managerHandle >= 0 else { return }
        guard let resp = lib.managerStatus(handle: managerHandle) else { return }
        if resp["reachable"] as? Bool == true {
            serverStatus = ServerStatusInfo(
                reachable: true,
                clientCount: resp["client_count"] as? Int ?? 0,
                tunnelCount: resp["tunnel_count"] as? Int ?? 0
            )
        }
    }

    func addForward(from: String, listen: String, to: String, service: String, proto: String = "tcp") {
        guard managerHandle >= 0 else { return }
        let params: [String: String] = [
            "client_name": from,
            "listen_addr": listen,
            "remote_client": to,
            "remote_service": service,
            "protocol": proto,
        ]
        guard let data = try? JSONSerialization.data(withJSONObject: params),
              let json = String(data: data, encoding: .utf8) else { return }
        let resp = lib.managerAddForward(handle: managerHandle, paramsJSON: json)
        if resp?["success"] as? Bool == false {
            errorMessage = resp?["message"] as? String
        } else {
            fetchForwards()
        }
    }

    func removeForward(from: String, listen: String) {
        guard managerHandle >= 0 else { return }
        let params: [String: String] = [
            "client_name": from,
            "listen_addr": listen,
        ]
        guard let data = try? JSONSerialization.data(withJSONObject: params),
              let json = String(data: data, encoding: .utf8) else { return }
        let resp = lib.managerRemoveForward(handle: managerHandle, paramsJSON: json)
        if resp?["success"] as? Bool == false {
            errorMessage = resp?["message"] as? String
        } else {
            fetchForwards()
        }
    }

    func kickClient(name: String) {
        guard managerHandle >= 0 else { return }
        let resp = lib.managerKickClient(handle: managerHandle, name: name)
        if resp?["success"] as? Bool == false {
            errorMessage = resp?["message"] as? String
        } else {
            fetchClients()
        }
    }
}

// MARK: - Data Models

struct ClientInfoItem: Identifiable {
    let id = UUID()
    var name: String
    var addr: String
    var services: String
    var connectedAt: String

    init(from dict: [String: Any]) {
        name = dict["name"] as? String ?? ""
        addr = dict["addr"] as? String ?? ""
        connectedAt = dict["connected_at"] as? String ?? ""
        if let svcs = dict["services"] as? [[String: Any]] {
            services = svcs.map { svc in
                let n = svc["name"] as? String ?? ""
                let p = svc["protocol"] as? String ?? ""
                let port = svc["port"] as? Int ?? 0
                return "\(n)(\(p):\(port))"
            }.joined(separator: ", ")
        } else {
            services = ""
        }
    }
}

struct TunnelInfoItem: Identifiable {
    let id = UUID()
    var sessionID: Int
    var sourceClient: String
    var targetClient: String
    var targetService: String
    var `protocol`: String

    init(from dict: [String: Any]) {
        sessionID = dict["session_id"] as? Int ?? 0
        sourceClient = dict["source_client"] as? String ?? ""
        targetClient = dict["target_client"] as? String ?? ""
        targetService = dict["target_service"] as? String ?? ""
        self.protocol = dict["protocol"] as? String ?? ""
    }
}

struct ForwardInfoItem: Identifiable {
    let id = UUID()
    var clientName: String
    var listenAddr: String
    var remoteClient: String
    var remoteService: String
    var `protocol`: String

    init(from dict: [String: Any]) {
        clientName = dict["client_name"] as? String ?? ""
        listenAddr = dict["listen_addr"] as? String ?? ""
        remoteClient = dict["remote_client"] as? String ?? ""
        remoteService = dict["remote_service"] as? String ?? ""
        self.protocol = dict["protocol"] as? String ?? ""
    }
}

struct ServerStatusInfo {
    var reachable: Bool
    var clientCount: Int
    var tunnelCount: Int
}
