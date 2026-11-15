#pragma once

#include <cstdint>
#include <filesystem>
#include <optional>
#include <string>

namespace rpd {

struct QualityMetrics {
    std::optional<double> sharpness;
    std::optional<double> contrast;
    std::optional<double> ink_ratio;
    std::optional<double> quality_score;
    bool blank{false};
    double deskew_angle{0.0};
};

struct PreprocessResult {
    std::filesystem::path output_path;
    std::filesystem::path quality_path;
    std::uintmax_t size_bytes{};
    std::string mode;
    QualityMetrics quality;
};

class ImageProcessor {
public:
    PreprocessResult preprocess(
        const std::filesystem::path& input,
        const std::filesystem::path& output_dir) const;

private:
    std::filesystem::path rasterize_pdf_first_page(
        const std::filesystem::path& input,
        const std::filesystem::path& output_dir) const;

    static bool is_pdf(const std::filesystem::path& input);
    static std::string shell_quote(const std::string& value);
};

} // namespace rpd
