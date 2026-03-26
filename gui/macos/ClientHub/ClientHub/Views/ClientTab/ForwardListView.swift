import SwiftUI

struct ForwardListView: View {
    @EnvironmentObject var appState: AppState

    @State private var showAddSheet = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Text("Port Forwards")
                    .font(.subheadline.bold())
                Spacer()
                Button {
                    showAddSheet = true
                } label: {
                    Image(systemName: "plus")
                }
                .buttonStyle(.borderless)
                .disabled(appState.connectionState.isRunning)
            }
            .padding(.horizontal)
            .padding(.vertical, 6)

            if appState.connectionState.forwards.isEmpty {
                Text("No forwards configured")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .center)
                    .padding()
            } else {
                Table(appState.connectionState.forwards) {
                    TableColumn("Listen", value: \.listenAddr)
                    TableColumn("Remote Client", value: \.remoteClient)
                    TableColumn("Service", value: \.remoteService)
                    TableColumn("Proto", value: \.protocol)
                        .width(50)
                    TableColumn("") { fwd in
                        Button(role: .destructive) {
                            appState.connectionState.forwards.removeAll { $0.id == fwd.id }
                        } label: {
                            Image(systemName: "trash")
                        }
                        .buttonStyle(.borderless)
                        .disabled(appState.connectionState.isRunning)
                    }
                    .width(30)
                }
                .frame(minHeight: 80, maxHeight: 150)
            }
        }
        .sheet(isPresented: $showAddSheet) {
            AddForwardSheet { listen, remote, service, proto in
                appState.connectionState.forwards.append(
                    ForwardItem(remoteClient: remote, remoteService: service, listenAddr: listen, protocol: proto)
                )
            }
        }
    }
}

struct AddForwardSheet: View {
    @Environment(\.dismiss) var dismiss
    @State private var listenAddr = ""
    @State private var remoteClient = ""
    @State private var remoteService = ""
    @State private var proto = "tcp"

    var onAdd: (String, String, String, String) -> Void

    var body: some View {
        VStack(spacing: 16) {
            Text("Add Port Forward")
                .font(.headline)

            Form {
                TextField("Listen Address:", text: $listenAddr)
                    .help("e.g. :13306 or 127.0.0.1:18080")
                TextField("Remote Client:", text: $remoteClient)
                TextField("Remote Service:", text: $remoteService)
                Picker("Protocol:", selection: $proto) {
                    Text("TCP").tag("tcp")
                    Text("UDP").tag("udp")
                }
                .pickerStyle(.segmented)
            }
            .formStyle(.grouped)

            HStack {
                Button("Cancel") { dismiss() }
                    .keyboardShortcut(.cancelAction)
                Spacer()
                Button("Add") {
                    onAdd(listenAddr, remoteClient, remoteService, proto)
                    dismiss()
                }
                .keyboardShortcut(.defaultAction)
                .disabled(listenAddr.isEmpty || remoteClient.isEmpty || remoteService.isEmpty)
            }
        }
        .padding()
        .frame(width: 360)
    }
}
