import Foundation

/// Change previews a command block's menu offers to review (#26): a port of
/// Go's review.Reviewable, kept in step by testdata/review/reviewable.json.
/// The parsing itself is Go's (the review service).
public enum Review {
    /// Whether a command line previews infrastructure changes:
    /// terraform/tofu/terragrunt plan, apply, show, destroy or run-all;
    /// kubectl or oc diff; helm diff.
    public static func reviewable(_ command: String) -> Bool {
        let words = Set(command.split(whereSeparator: { $0 == " " || $0 == "\t" || $0 == "\n" }).map(String.init))
        func has(_ list: String...) -> Bool { list.contains(where: words.contains) }
        if has("terraform", "tofu", "terragrunt"), has("plan", "apply", "show", "destroy", "run-all") { return true }
        if has("kubectl", "oc"), has("diff") { return true }
        if has("helm"), has("diff") { return true }
        return false
    }
}
