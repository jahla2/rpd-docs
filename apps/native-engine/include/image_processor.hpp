#pragma once

#include <cstdint>
#include <filesystem>
#include <optional>
#include <string>
#include <vector>

namespace rpd {

struct QualityMetrics {
    std::optional<double> sharpness;
    std::optional<double> contrast;
    std::optional<double> ink_ratio;
    std::optional<double> quality_score;
    bool blank{false};
    double deskew_angle{0.0};
};

struct PreparedPage {
    int page{1};
    std::filesystem::path image_path;
    std::filesystem::path quality_path;
    std::optional<std::filesystem::path> text_layout_path;
    std::uintmax_t size_bytes{};
    std::string mode;
    bool searchable_text{false};
    QualityMetrics quality;
};

struct PrepareResult {
    std::string schema_version{"2.0"};
    int page_count{0};
    std::vector<PreparedPage> pages;
};

class ImageProcessor {
public:
    PrepareResult prepare_document(
        const std::filesystem::path& input,
        const std::filesystem::path& output_dir) const;

    // Backward-compatible single-page operation used by existing scripts.
    PreparedPage preprocess(
        const std::filesystem::path& input,
        const std::filesystem::path& output_dir) const;

private:
    PreparedPage preprocess_image(
        const std::filesystem::path& source,
        const std::filesystem::path& output_dir,
        int page,
        const std::string& mode_prefix) const;

    std::filesystem::path rasterize_pdf_page(
        const std::filesystem::path& input,
        const std::filesystem::path& output_dir,
        int page) const;

    std::optional<std::filesystem::path> extract_pdf_text_layout(
        const std::filesystem::path& input,
        const std::filesystem::path& output_dir,
        int page) const;

    int pdf_page_count(
        const std::filesystem::path& input,
        const std::filesystem::path& output_dir) const;

    static bool is_pdf(const std::filesystem::path& input);
    static bool has_searchable_text(const std::filesystem::path& layout_path);
    static std::string shell_quote(const std::string& value);
    static std::string page_token(int page);
};

} // namespace rpd
