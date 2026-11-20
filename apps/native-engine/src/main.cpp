#include "image_processor.hpp"

#include <iomanip>
#include <iostream>
#include <optional>
#include <stdexcept>
#include <string>

namespace {

void write_optional_number(const std::optional<double>& value) {
    if (value.has_value()) {
        std::cout << std::fixed << std::setprecision(6) << *value;
    } else {
        std::cout << "null";
    }
}

void write_page(const rpd::PreparedPage& page) {
    std::cout
        << "{"
        << "\"page\":" << page.page << ","
        << "\"image_artifact\":" << std::quoted(page.image_path.string()) << ","
        << "\"quality_artifact\":" << std::quoted(page.quality_path.string()) << ","
        << "\"text_layout_artifact\":";
    if (page.text_layout_path.has_value()) {
        std::cout << std::quoted(page.text_layout_path->string());
    } else {
        std::cout << "null";
    }
    std::cout
        << ",\"size_bytes\":" << page.size_bytes
        << ",\"mode\":" << std::quoted(page.mode)
        << ",\"searchable_text\":" << (page.searchable_text ? "true" : "false")
        << ",\"deskew_angle\":" << std::fixed << std::setprecision(4) << page.quality.deskew_angle
        << ",\"quality_score\":";
    write_optional_number(page.quality.quality_score);
    std::cout << ",\"blank\":" << (page.quality.blank ? "true" : "false") << "}";
}

} // namespace

int main(int argc, char** argv) {
    if (argc != 4) {
        std::cerr << "usage: rpd-native-engine <prepare|preprocess> <input> <output-dir>\n";
        return 2;
    }

    const std::string command = argv[1];
    if (command != "prepare" && command != "preprocess") {
        std::cerr << "unknown command: " << command << "\n";
        return 2;
    }

    try {
        rpd::ImageProcessor processor;

        if (command == "preprocess") {
            const auto page = processor.preprocess(argv[2], argv[3]);
            std::cout
                << "{"
                << "\"schema_version\":\"1.0\","
                << "\"output_artifact\":" << std::quoted(page.image_path.string()) << ","
                << "\"quality_artifact\":" << std::quoted(page.quality_path.string()) << ","
                << "\"size_bytes\":" << page.size_bytes << ","
                << "\"mode\":" << std::quoted(page.mode) << ","
                << "\"deskew_angle\":" << std::fixed << std::setprecision(4) << page.quality.deskew_angle << ","
                << "\"quality_score\":";
            write_optional_number(page.quality.quality_score);
            std::cout << ",\"blank\":" << (page.quality.blank ? "true" : "false") << "}\n";
            return 0;
        }

        const auto result = processor.prepare_document(argv[2], argv[3]);
        std::cout
            << "{"
            << "\"schema_version\":\"2.0\","
            << "\"page_count\":" << result.page_count << ","
            << "\"pages\":[";
        for (std::size_t i = 0; i < result.pages.size(); ++i) {
            if (i > 0) {
                std::cout << ",";
            }
            write_page(result.pages[i]);
        }
        std::cout << "]}\n";
        return 0;
    } catch (const std::exception& ex) {
        std::cerr << "native processing failed: " << ex.what() << "\n";
        return 1;
    }
}
