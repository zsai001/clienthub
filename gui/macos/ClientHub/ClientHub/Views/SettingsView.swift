import SwiftUI
import UniformTypeIdentifiers

struct SettingsView: View {
    @EnvironmentObject var appState: AppState
    @State private var importError: String?
    @State private var exportSuccess = false

    var body: some View {
        TabView {
            configTab
                .tabItem {
                    Label("Config", systemImage: "doc.text")
                }

            aboutTab
                .tabItem {
                    Label("About", systemImage: "info.circle")
                }
        }
        .frame(width: 480, height: 320)
    }

    // MARK: - Config Tab

    private var configTab: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Configuration")
                .font(.headline)

            GroupBox("Import") {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Load connection settings from a YAML config file.")
                        .font(.caption)
                        .foregroundStyle(.secondary)

                    HStack {
                        Button("Import Client Config...") {
                            importClientConfig()
                        }
                        Spacer()
                    }

                    if let err = importError {
                        Text(err)
                            .font(.caption)
                            .foregroundStyle(.red)
                    }
                }
                .padding(4)
            }

            GroupBox("Export") {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Save current connection settings to a YAML file.")
                        .font(.caption)
                        .foregroundStyle(.secondary)

                    HStack {
                        Button("Export Client Config...") {
                            exportClientConfig()
                        }
                        .disabled(appState.connectionState.serverAddr.isEmpty)
                        Spacer()
                        if exportSuccess {
                            Text("Saved!")
                                .font(.caption)
                                .foregroundStyle(.green)
                        }
                    }
                }
                .padding(4)
            }

            Spacer()
        }
        .padding()
    }

    // MARK: - About Tab

    private var aboutTab: some View {
        VStack(spacing: 12) {
            Image(systemName: "network")
                .font(.system(size: 48))
                .foregroundStyle(.blue)

            Text("ClientHub")
                .font(.title2.bold())

            Text("Port Forwarding Service")
                .font(.subheadline)
                .foregroundStyle(.secondary)

            Text("Encrypted tunnels with XChaCha20-Poly1305")
                .font(.caption)
                .foregroundStyle(.secondary)

            Spacer()
        }
        .padding()
        .frame(maxWidth: .infinity)
    }

    // MARK: - Import / Export

    private func importClientConfig() {
        let panel = NSOpenPanel()
        panel.allowedContentTypes = [UTType(filenameExtension: "yaml")!, UTType(filenameExtension: "yml")!]
        panel.allowsMultipleSelection = false
        panel.canChooseDirectories = false

        guard panel.runModal() == .OK, let url = panel.url else { return }

        do {
            let content = try String(contentsOf: url, encoding: .utf8)
            parseYAMLConfig(content)
            importError = nil
        } catch {
            importError = "Failed to read file: \(error.localizedDescription)"
        }
    }

    private func parseYAMLConfig(_ content: String) {
        // Simple YAML key-value parser for flat client config
        var dict: [String: String] = [:]
        for line in content.components(separatedBy: .newlines) {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if trimmed.isEmpty || trimmed.hasPrefix("#") { continue }
            let parts = trimmed.split(separator: ":", maxSplits: 1)
            if parts.count == 2 {
                let key = parts[0].trimmingCharacters(in: .whitespaces)
                let val = parts[1].trimmingCharacters(in: .whitespaces).trimmingCharacters(in: CharacterSet(charactersIn: "\"'"))
                dict[key] = val
            }
        }

        if let v = dict["server_addr"] { appState.connectionState.serverAddr = v }
        if let v = dict["client_name"] { appState.connectionState.clientName = v }
        if let v = dict["secret"] { appState.connectionState.secret = v }
    }

    private func exportClientConfig() {
        let panel = NSSavePanel()
        panel.allowedContentTypes = [UTType(filenameExtension: "yaml")!]
        panel.nameFieldStringValue = "\(appState.connectionState.clientName).yaml"

        guard panel.runModal() == .OK, let url = panel.url else { return }

        var lines = [
            "server_addr: \"\(appState.connectionState.serverAddr)\"",
            "client_name: \"\(appState.connectionState.clientName)\"",
            "secret: \"\(appState.connectionState.secret)\"",
        ]

        if !appState.connectionState.exposeServices.isEmpty {
            lines.append("expose:")
            for svc in appState.connectionState.exposeServices {
                lines.append("  - name: \"\(svc.name)\"")
                lines.append("    local_addr: \"\(svc.localAddr)\"")
                lines.append("    protocol: \"\(svc.protocol)\"")
            }
        }

        if !appState.connectionState.forwards.isEmpty {
            lines.append("forward:")
            for fwd in appState.connectionState.forwards {
                lines.append("  - remote_client: \"\(fwd.remoteClient)\"")
                lines.append("    remote_service: \"\(fwd.remoteService)\"")
                lines.append("    listen_addr: \"\(fwd.listenAddr)\"")
                lines.append("    protocol: \"\(fwd.protocol)\"")
            }
        }

        do {
            try lines.joined(separator: "\n").write(to: url, atomically: true, encoding: .utf8)
            exportSuccess = true
            DispatchQueue.main.asyncAfter(deadline: .now() + 2) {
                exportSuccess = false
            }
        } catch {
            importError = "Failed to save: \(error.localizedDescription)"
        }
    }
}
