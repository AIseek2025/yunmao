import SwiftUI
#if canImport(StoreKit)
import StoreKit
#endif

public struct ProfileView: View {
    @EnvironmentObject var session: SessionStore
    @State private var wallet: Wallet?
    @State private var error: String?
    #if canImport(StoreKit)
    @State private var products: [Product] = []
    @State private var storeKit: StoreKitManager?
    #endif

    public init() {}

    public var body: some View {
        NavigationStack {
            List {
                Section(header: Text("我的")) {
                    if let u = session.auth?.userId {
                        Text("用户：\(u)")
                    }
                    if let w = wallet {
                        HStack { Text("钱包余额"); Spacer(); Text("\(w.balanceFen / 100) 元") }
                        HStack { Text("代币"); Spacer(); Text("\(w.coins) coin") }
                    }
                }
                #if canImport(StoreKit)
                Section(header: Text("充值")) {
                    ForEach(products, id: \.id) { p in
                        Button {
                            Task { await purchase(p) }
                        } label: {
                            HStack {
                                VStack(alignment: .leading) {
                                    Text(p.displayName).font(.body)
                                    Text(p.description).font(.caption).foregroundColor(.secondary)
                                }
                                Spacer()
                                Text(p.displayPrice)
                            }
                        }
                    }
                }
                #endif
                Section {
                    Button("退出登录", role: .destructive) {
                        Task { await session.logout() }
                    }
                }
                if let error {
                    Text(error).foregroundColor(.red).font(.footnote)
                }
            }
            .navigationTitle("我的")
            .task { await load() }
        }
    }

    private func load() async {
        guard let uid = session.auth?.userId else { return }
        do {
            self.wallet = try await session.api.wallet(userID: uid)
        } catch {
            self.error = String(describing: error)
        }
        #if canImport(StoreKit)
        let mgr = StoreKitManager(api: session.api)
        self.storeKit = mgr
        do {
            try await mgr.fetchProducts()
            products = await mgr.products
        } catch {
            self.error = String(describing: error)
        }
        #endif
    }

    #if canImport(StoreKit)
    private func purchase(_ p: Product) async {
        guard let storeKit else { return }
        let res = await storeKit.purchase(p)
        switch res {
        case .success(let tid):
            self.error = "已发起充值 #\(tid)，等待账户更新"
        case .userCancelled: break
        case .pending:
            self.error = "购买待审核"
        case .failed(let e):
            self.error = "失败：\(e)"
        }
    }
    #endif
}
