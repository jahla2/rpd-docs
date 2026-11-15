#pragma once

#include <cstdint>
#include <filesystem>

namespace rpd {

struct PreprocessResult {
    std::filesystem::path output_path;
    std::filesystem::path quality_path;
    std::uintmax_t size_bytes{};
};

class ImageProcessor {
public:
    PreprocessResult preprocess(
        const std::filesystem::path& input,
        const std::filesystem::path& output_dir) const;
};

} // namespace rpd
