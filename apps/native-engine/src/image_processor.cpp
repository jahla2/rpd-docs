#include "image_processor.hpp"

#include <algorithm>
#include <cctype>
#include <cmath>
#include <cstdlib>
#include <fstream>
#include <iomanip>
#include <sstream>
#include <stdexcept>
#include <string>
#include <vector>

#ifdef RPD_WITH_OPENCV
#include <opencv2/core.hpp>
#include <opencv2/imgcodecs.hpp>
#include <opencv2/imgproc.hpp>
#include <opencv2/photo.hpp>
#endif

namespace rpd {
namespace {

void write_quality_json(
    const std::filesystem::path& path,
    const std::string& mode,
    const QualityMetrics& q) {

    std::ofstream out(path);
    if (!out) {
        throw std::runtime_error("failed to write quality manifest");
    }

    auto number_or_null = [](const std::optional<double>& value) {
        if (!value.has_value()) {
            return std::string("null");
        }
        std::ostringstream ss;
        ss << std::fixed << std::setprecision(6) << *value;
        return ss.str();
    };

    out << "{\n"
        << "  \"schema_version\": \"1.0\",\n"
        << "  \"preprocessing\": \"" << mode << "\",\n"
        << "  \"deskew_angle\": " << std::fixed << std::setprecision(4) << q.deskew_angle << ",\n"
        << "  \"sharpness\": " << number_or_null(q.sharpness) << ",\n"
        << "  \"contrast\": " << number_or_null(q.contrast) << ",\n"
        << "  \"ink_ratio\": " << number_or_null(q.ink_ratio) << ",\n"
        << "  \"quality_score\": " << number_or_null(q.quality_score) << ",\n"
        << "  \"blank\": " << (q.blank ? "true" : "false") << "\n"
        << "}\n";
}

#ifdef RPD_WITH_OPENCV

double laplacian_variance(const cv::Mat& gray) {
    cv::Mat lap;
    cv::Laplacian(gray, lap, CV_64F);
    cv::Scalar mean, stddev;
    cv::meanStdDev(lap, mean, stddev);
    return stddev[0] * stddev[0];
}

double contrast_stddev(const cv::Mat& gray) {
    cv::Scalar mean, stddev;
    cv::meanStdDev(gray, mean, stddev);
    return stddev[0];
}

double calculate_ink_ratio(const cv::Mat& gray) {
    cv::Mat thresholded;
    cv::threshold(gray, thresholded, 0, 255, cv::THRESH_BINARY_INV | cv::THRESH_OTSU);
    return static_cast<double>(cv::countNonZero(thresholded)) /
           static_cast<double>(std::max<std::size_t>(1, gray.total()));
}

double estimate_skew(const cv::Mat& gray) {
    cv::Mat binary;
    cv::threshold(gray, binary, 0, 255, cv::THRESH_BINARY_INV | cv::THRESH_OTSU);

    std::vector<cv::Point> points;
    cv::findNonZero(binary, points);
    if (points.size() < 100) {
        return 0.0;
    }

    const cv::RotatedRect box = cv::minAreaRect(points);
    double angle = box.angle;
    if (angle < -45.0) {
        angle += 90.0;
    } else if (angle > 45.0) {
        angle -= 90.0;
    }
    if (std::abs(angle) > 15.0) {
        return 0.0;
    }
    return angle;
}

cv::Mat rotate_keep_size(const cv::Mat& input, double angle) {
    if (std::abs(angle) < 0.05) {
        return input.clone();
    }

    const cv::Point2f center(
        static_cast<float>(input.cols) / 2.0F,
        static_cast<float>(input.rows) / 2.0F);

    cv::Mat matrix = cv::getRotationMatrix2D(center, angle, 1.0);
    cv::Mat rotated;
    cv::warpAffine(
        input,
        rotated,
        matrix,
        input.size(),
        cv::INTER_CUBIC,
        cv::BORDER_REPLICATE);
    return rotated;
}
#endif

} // namespace

std::string ImageProcessor::page_token(int page) {
    std::ostringstream out;
    out << std::setw(3) << std::setfill('0') << page;
    return out.str();
}

bool ImageProcessor::is_pdf(const std::filesystem::path& input) {
    std::string ext = input.extension().string();
    std::transform(ext.begin(), ext.end(), ext.begin(), [](unsigned char c) {
        return static_cast<char>(std::tolower(c));
    });
    return ext == ".pdf";
}

std::string ImageProcessor::shell_quote(const std::string& value) {
#ifdef _WIN32
    std::string quoted = "\"";
    for (char c : value) {
        if (c == '\"') {
            quoted += "\\\"";
        } else {
            quoted += c;
        }
    }
    quoted += "\"";
    return quoted;
#else
    std::string quoted = "'";
    for (char c : value) {
        if (c == '\'') {
            quoted += "'\\''";
        } else {
            quoted += c;
        }
    }
    quoted += "'";
    return quoted;
#endif
}

int ImageProcessor::pdf_page_count(
    const std::filesystem::path& input,
    const std::filesystem::path& output_dir) const {

    const auto info_path = output_dir / "pdfinfo.txt";
#ifdef _WIN32
    const std::string redirect = " > ";
#else
    const std::string redirect = " > ";
#endif
    const std::string command =
        "pdfinfo " + shell_quote(input.string()) +
        redirect + shell_quote(info_path.string());

    const int code = std::system(command.c_str());
    if (code != 0 || !std::filesystem::exists(info_path)) {
        throw std::runtime_error(
            "PDF inspection failed. Ensure Poppler/pdfinfo is installed and available on PATH");
    }

    std::ifstream in(info_path);
    std::string line;
    while (std::getline(in, line)) {
        if (line.rfind("Pages:", 0) == 0) {
            std::istringstream ss(line.substr(6));
            int pages = 0;
            ss >> pages;
            if (pages > 0) {
                return pages;
            }
        }
    }
    throw std::runtime_error("PDF page count could not be determined");
}

std::filesystem::path ImageProcessor::rasterize_pdf_page(
    const std::filesystem::path& input,
    const std::filesystem::path& output_dir,
    int page) const {

    const auto prefix = output_dir / ("raster-page-" + page_token(page));
    const auto output = prefix.string() + ".png";

    const std::string page_value = std::to_string(page);
    const std::string command =
        "pdftoppm -f " + page_value +
        " -l " + page_value +
        " -singlefile -r 200 -png " +
        shell_quote(input.string()) + " " +
        shell_quote(prefix.string());

    const int code = std::system(command.c_str());
    if (code != 0 || !std::filesystem::exists(output)) {
        throw std::runtime_error(
            "PDF rasterization failed for page " + page_value +
            ". Ensure Poppler/pdftoppm is installed and available on PATH");
    }
    return std::filesystem::path(output);
}

std::optional<std::filesystem::path> ImageProcessor::extract_pdf_text_layout(
    const std::filesystem::path& input,
    const std::filesystem::path& output_dir,
    int page) const {

    const auto layout_path = output_dir / ("page-" + page_token(page) + "-bbox.html");
    const std::string page_value = std::to_string(page);
    const std::string command =
        "pdftotext -f " + page_value +
        " -l " + page_value +
        " -bbox-layout " +
        shell_quote(input.string()) + " " +
        shell_quote(layout_path.string());

    const int code = std::system(command.c_str());
    if (code != 0 || !std::filesystem::exists(layout_path)) {
        return std::nullopt;
    }
    return std::filesystem::absolute(layout_path);
}

bool ImageProcessor::has_searchable_text(const std::filesystem::path& layout_path) {
    std::ifstream in(layout_path, std::ios::binary);
    if (!in) {
        return false;
    }
    const std::string content(
        (std::istreambuf_iterator<char>(in)),
        std::istreambuf_iterator<char>());

    std::size_t words = 0;
    std::size_t position = 0;
    while ((position = content.find("<word ", position)) != std::string::npos) {
        ++words;
        position += 6;
        if (words >= 8) {
            return true;
        }
    }
    return false;
}

PreparedPage ImageProcessor::preprocess_image(
    const std::filesystem::path& source,
    const std::filesystem::path& output_dir,
    int page,
    const std::string& mode_prefix) const {

    const std::string token = page_token(page);
#ifdef RPD_WITH_OPENCV
    cv::Mat original = cv::imread(source.string(), cv::IMREAD_COLOR);
    if (original.empty()) {
        throw std::runtime_error("OpenCV could not decode input image");
    }

    cv::Mat gray;
    cv::cvtColor(original, gray, cv::COLOR_BGR2GRAY);
    const double skew = estimate_skew(gray);
    cv::Mat deskewed = rotate_keep_size(original, skew);

    cv::Mat deskewed_gray;
    cv::cvtColor(deskewed, deskewed_gray, cv::COLOR_BGR2GRAY);

    cv::Mat denoised;
    cv::fastNlMeansDenoising(deskewed_gray, denoised, 5.0F, 7, 21);

    auto clahe = cv::createCLAHE(2.0, cv::Size(8, 8));
    cv::Mat enhanced;
    clahe->apply(denoised, enhanced);

    cv::Mat processed = enhanced;
    const int min_dimension = std::min(processed.cols, processed.rows);
    if (min_dimension > 0 && min_dimension < 1200) {
        const double scale = 1200.0 / static_cast<double>(min_dimension);
        cv::resize(processed, processed, cv::Size(), scale, scale, cv::INTER_CUBIC);
    }
    const int max_dimension = std::max(processed.cols, processed.rows);
    if (max_dimension > 5000) {
        const double scale = 5000.0 / static_cast<double>(max_dimension);
        cv::resize(processed, processed, cv::Size(), scale, scale, cv::INTER_AREA);
    }

    QualityMetrics quality;
    quality.deskew_angle = skew;
    quality.sharpness = laplacian_variance(processed);
    quality.contrast = contrast_stddev(processed);
    quality.ink_ratio = calculate_ink_ratio(processed);
    quality.blank = *quality.ink_ratio < 0.0025;

    const double sharpness_score = std::clamp(*quality.sharpness / 800.0, 0.0, 1.0);
    const double contrast_score = std::clamp(*quality.contrast / 64.0, 0.0, 1.0);
    const double ink_score = std::clamp(*quality.ink_ratio / 0.08, 0.0, 1.0);
    quality.quality_score = quality.blank
        ? 0.0
        : (sharpness_score * 0.50 + contrast_score * 0.35 + ink_score * 0.15);

    const auto output = output_dir / ("page-" + token + ".png");
    if (!cv::imwrite(output.string(), processed)) {
        throw std::runtime_error("OpenCV failed to write preprocessed page");
    }

    const auto quality_path = output_dir / ("page-" + token + "-quality.json");
    const std::string mode = mode_prefix + "+opencv";
    write_quality_json(quality_path, mode, quality);

    return PreparedPage{
        .page = page,
        .image_path = std::filesystem::absolute(output),
        .quality_path = std::filesystem::absolute(quality_path),
        .text_layout_path = std::nullopt,
        .size_bytes = std::filesystem::file_size(output),
        .mode = mode,
        .searchable_text = false,
        .quality = quality
    };
#else
    auto extension = source.extension();
    if (extension.empty()) {
        extension = ".bin";
    }

    const auto output = output_dir / ("page-" + token + extension.string());
    std::filesystem::copy_file(
        source,
        output,
        std::filesystem::copy_options::overwrite_existing);

    QualityMetrics quality;
    const auto quality_path = output_dir / ("page-" + token + "-quality.json");
    const std::string mode = mode_prefix + "+passthrough-no-opencv";
    write_quality_json(quality_path, mode, quality);

    return PreparedPage{
        .page = page,
        .image_path = std::filesystem::absolute(output),
        .quality_path = std::filesystem::absolute(quality_path),
        .text_layout_path = std::nullopt,
        .size_bytes = std::filesystem::file_size(output),
        .mode = mode,
        .searchable_text = false,
        .quality = quality
    };
#endif
}

PrepareResult ImageProcessor::prepare_document(
    const std::filesystem::path& input,
    const std::filesystem::path& output_dir) const {

    if (!std::filesystem::exists(input) || !std::filesystem::is_regular_file(input)) {
        throw std::runtime_error("input file does not exist");
    }
    std::filesystem::create_directories(output_dir);

    PrepareResult result;

    if (!is_pdf(input)) {
        result.pages.push_back(preprocess_image(input, output_dir, 1, "image"));
        result.page_count = 1;
        return result;
    }

    const int pages = pdf_page_count(input, output_dir);
    result.page_count = pages;
    result.pages.reserve(static_cast<std::size_t>(pages));

    for (int page = 1; page <= pages; ++page) {
        const auto rasterized = rasterize_pdf_page(input, output_dir, page);
        auto prepared = preprocess_image(rasterized, output_dir, page, "pdf-rasterized");

        const auto layout = extract_pdf_text_layout(input, output_dir, page);
        if (layout.has_value() && has_searchable_text(*layout)) {
            prepared.text_layout_path = layout;
            prepared.searchable_text = true;
            prepared.mode += "+embedded-text";
        }
        result.pages.push_back(std::move(prepared));
    }
    return result;
}

PreparedPage ImageProcessor::preprocess(
    const std::filesystem::path& input,
    const std::filesystem::path& output_dir) const {

    auto result = prepare_document(input, output_dir);
    if (result.pages.empty()) {
        throw std::runtime_error("document produced no pages");
    }
    return result.pages.front();
}

} // namespace rpd
