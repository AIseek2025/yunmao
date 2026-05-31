// StoreKitManager：StoreKit 2 in-app purchase。
//
// 产品 ID 与后端约定：
//   - com.yunmao.coin.small   (60 coin)
//   - com.yunmao.coin.medium  (300 coin)
//   - com.yunmao.coin.large   (1200 coin)
//
// 流程：
//   1. fetchProducts → 显示价格 / title
//   2. purchase(product) → 拿 Transaction
//   3. 把 transaction.jwsRepresentation 上报后端 webhook /api/v1/pay/webhook/appleiap
//   4. transaction.finish()

#if canImport(StoreKit)
import Foundation
import StoreKit

@available(iOS 15.0, *)
public actor StoreKitManager {
    public static let productIDs: Set<String> = [
        "com.yunmao.coin.small",
        "com.yunmao.coin.medium",
        "com.yunmao.coin.large",
    ]

    public private(set) var products: [Product] = []
    private let api: YunmaoAPI

    public init(api: YunmaoAPI) {
        self.api = api
    }

    public func fetchProducts() async throws {
        let res = try await Product.products(for: Array(Self.productIDs))
        self.products = res.sorted { $0.price < $1.price }
    }

    public enum PurchaseResult {
        case success(transactionID: UInt64)
        case userCancelled
        case pending
        case failed(String)
    }

    public func purchase(_ product: Product) async -> PurchaseResult {
        do {
            let result = try await product.purchase()
            switch result {
            case .success(let verification):
                switch verification {
                case .verified(let transaction):
                    // 上报后端 webhook：VerificationResult 提供 JWS 原文。
                    let jws = verification.jwsRepresentation
                    let payload: [String: Any] = [
                        "signedPayload": jws,
                    ]
                    let data = try JSONSerialization.data(withJSONObject: payload)
                    do {
                        _ = try await api.appleIAPWebhook(payload: data)
                    } catch {
                        // 上报失败：保留 unfinished，下次启动 Transaction.unfinished 重试
                        return .failed("upload receipt: \(error)")
                    }
                    await transaction.finish()
                    return .success(transactionID: transaction.id)
                case .unverified(_, let err):
                    return .failed("unverified: \(err)")
                }
            case .userCancelled:
                return .userCancelled
            case .pending:
                return .pending
            @unknown default:
                return .failed("unknown result")
            }
        } catch {
            return .failed(error.localizedDescription)
        }
    }

    public func listenForUpdates() async {
        for await update in Transaction.updates {
            if case .verified(let tx) = update {
                let jws = update.jwsRepresentation
                let payload: [String: Any] = ["signedPayload": jws]
                if let data = try? JSONSerialization.data(withJSONObject: payload) {
                    _ = try? await api.appleIAPWebhook(payload: data)
                }
                await tx.finish()
            }
        }
    }
}
#endif
