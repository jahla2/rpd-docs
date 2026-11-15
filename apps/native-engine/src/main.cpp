#include "image_processor.hpp"

#include <iomanip>
#include <iostream>
#include <stdexcept>
#include <string>

int main(int argc, char** argv) {
    if (argc != 4 || std::string(argv[1]) != "preprocess") {
        std::cerr << "usage: rpd-native-engine preprocess <input> <output-dir>\n";
        return 2;
    }

    try {
        rpd::ImageProcessor processor;
        const auto result = processor.preprocess(argv[2], argv[3]);

        std::cout
            << "{"
            << "\"schema_version\":\"1.0\","
            << "\"output_artifact\":" << std::quoted(result.output_path.string()) << ","
            << "\"quality_artifact\":" << std::quoted(result.quality_path.string()) << ","
            << "\"size_bytes\":" << result.size_bytes
            << "}\n";
        return 0;
    } catch (const std::exception& ex) {
        std::cerr << "native preprocess failed: " << ex.what() << "\n";
        return 1;
    }
}
