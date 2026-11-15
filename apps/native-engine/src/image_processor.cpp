#include "image_processor.hpp"

#include <fstream>
#include <stdexcept>

namespace rpd {

PreprocessResult ImageProcessor::preprocess(
    const std::filesystem::path& input,
    const std::filesystem::path& output_dir) const {

    if (!std::filesystem::exists(input) || !std::filesystem::is_regular_file(input)) {
        throw std::runtime_error("input file does not exist");
    }

    std::filesystem::create_directories(output_dir);

    auto extension = input.extension();
    if (extension.empty()) {
        extension = ".bin";
    }

    const auto output = output_dir / ("page-001" + extension.string());
    std::filesystem::copy_file(
        input,
        output,
        std::filesystem::copy_options::overwrite_existing);

    const auto quality = output_dir / "quality.json";
    std::ofstream q(quality);
    if (!q) {
        throw std::runtime_error("failed to write quality manifest");
    }

    // Foundation slice: preserves input bytes while defining the native-engine
    // contract. OpenCV/Poppler deskew, denoise and PDF rasterization are Phase 2.
    q << "{\n"
      << "  \"schema_version\": \"1.0\",\n"
      << "  \"preprocessing\": \"passthrough-foundation\",\n"
      << "  \"rotation\": 0,\n"
      << "  \"deskew_angle\": 0.0,\n"
      << "  \"quality_score\": null,\n"
      << "  \"blank\": false\n"
      << "}\n";

    return PreprocessResult{
        .output_path = std::filesystem::absolute(output),
        .quality_path = std::filesystem::absolute(quality),
        .size_bytes = std::filesystem::file_size(output)
    };
}

} // namespace rpd
