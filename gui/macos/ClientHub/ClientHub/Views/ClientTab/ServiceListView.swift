import SwiftUI

struct ServiceListView: View {
    @EnvironmentObject var appState: AppState

    @State private var showAddSheet = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Text("Exposed Services")
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

            if appState.connectionState.exposeServices.isEmpty {
                Text("No services exposed")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .center)
                    .padding()
            } else {
                Table(appState.connectionState.exposeServices) {
                    TableColumn("Name", value: \.name)
                    TableColumn("Address", value: \.localAddr)
                    TableColumn("Protocol", value: \.protocol)
                        .width(60)
                    TableColumn("") { svc in
                        Button(role: .destructive) {
                            appState.connectionState.exposeServices.removeAll { $0.id == svc.id }
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
            AddServiceSheet { name, addr, proto in
                appState.connectionState.exposeServices.append(
                    ExposeServiceItem(name: name, localAddr: addr, protocol: proto)
                )
            }
        }
    }
}

struct AddServiceSheet: View {
    @Environment(\.dismiss) var dismiss
    @State private var name = ""
    @State private var localAddr = ""
    @State private var proto = "tcp"

    var onAdd: (String, String, String) -> Void

    var body: some View {
        VStack(spacing: 16) {
            Text("Add Exposed Service")
                .font(.headline)

            Form {
                TextField("Service Name:", text: $name)
                TextField("Local Address:", text: $localAddr)
                    .help("e.g. 127.0.0.1:8080")
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
                    onAdd(name, localAddr, proto)
                    dismiss()
                }
                .keyboardShortcut(.defaultAction)
                .disabled(name.isEmpty || localAddr.isEmpty)
            }
        }
        .padding()
        .frame(width: 360)
    }
}
